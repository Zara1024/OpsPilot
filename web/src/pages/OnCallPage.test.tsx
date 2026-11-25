import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import * as oncallApi from '@/api/oncall';
import * as usersApi from '@/api/users';
import OnCallPage, { formatCountdownText, resolveHandoffTargetMs } from './OnCallPage';

vi.mock('@/api/oncall', async () => {
  const actual = await vi.importActual<typeof import('@/api/oncall')>('@/api/oncall');
  return {
    ...actual,
    listSchedules: vi.fn(),
    listPendingOverrides: vi.fn(),
    listEscalationPolicies: vi.fn(),
    getCalendarShifts: vi.fn(),
    getCurrentOnCall: vi.fn(),
    listRoutes: vi.fn(),
  };
});

vi.mock('@/api/users', async () => {
  const actual = await vi.importActual<typeof import('@/api/users')>('@/api/users');
  return {
    ...actual,
    listUsers: vi.fn(),
  };
});

const schedule: oncallApi.OnCallSchedule = {
  id: 1,
  team_id: 1,
  name: 'Core SRE Primary',
  description: '24x7 rotation',
  timezone: 'Asia/Shanghai',
  handoff_time: '09:30',
  reminder_advance_hours: 12,
  require_swap_approval: true,
  enabled: true,
  created_by: 1,
  created_at: '2026-11-01T00:00:00Z',
  updated_at: '2026-11-01T00:00:00Z',
};

const route: oncallApi.OnCallRoute = {
  id: 42,
  team_id: 1,
  name: 'Payment P1 Route',
  matcher_json: '[]',
  matchers: [{ name: 'service', operator: '=', value: 'payment' }],
  priority: 10,
  schedule_id: 1,
  group_wait_seconds: 30,
  group_interval_seconds: 300,
  enabled: true,
  created_at: '2026-11-01T00:00:00Z',
  updated_at: '2026-11-01T00:00:00Z',
};

const primarySlot: oncallApi.ShiftSlot = {
  schedule_id: 1,
  schedule_name: 'Core SRE Primary',
  rotation_id: 10,
  rotation_name: 'Primary Rotation',
  tier: 1,
  user_id: 1,
  user_name: 'Alice',
  user_email: 'alice@example.com',
  user_phone: '13800000001',
  start_time: '2026-11-25T01:30:00Z',
  end_time: '2026-11-26T01:30:00Z',
  is_override: false,
};

const secondarySlot: oncallApi.ShiftSlot = {
  ...primarySlot,
  rotation_id: 11,
  rotation_name: 'Secondary Escalation Tier',
  tier: 2,
  user_id: 2,
  user_name: 'Bob',
  user_email: 'bob@example.com',
  user_phone: '13800000002',
};

describe('OnCall countdown helpers', () => {
  const nowMs = Date.UTC(2026, 10, 25, 4, 0, 0);

  it('formats a valid future ISO handoff_time as HH:MM:SS', () => {
    const target = resolveHandoffTargetMs(
      {
        schedule_id: 1,
        schedule_name: 'Core SRE Primary',
        remaining_seconds: 0,
        handoff_time: new Date(nowMs + 3661 * 1000).toISOString(),
      },
      nowMs,
    );
    expect(target).toBe(nowMs + 3661 * 1000);
    expect(formatCountdownText(target, nowMs, 'Handoff time reached')).toBe('01:01:01');
  });

  it('falls back to remaining_seconds when handoff_time is a time-of-day string', () => {
    const target = resolveHandoffTargetMs(
      {
        schedule_id: 1,
        schedule_name: 'Core SRE Primary',
        remaining_seconds: 3665,
        handoff_time: '09:30',
      },
      nowMs,
    );
    expect(target).toBe(nowMs + 3665 * 1000);
    expect(formatCountdownText(target, nowMs, 'Handoff time reached')).toBe('01:01:05');
  });

  it('returns empty string instead of NaN when handoff_time is unparseable and remaining_seconds is 0', () => {
    const target = resolveHandoffTargetMs(
      {
        schedule_id: 1,
        schedule_name: 'Core SRE Primary',
        remaining_seconds: 0,
        handoff_time: '09:30',
      },
      nowMs,
    );
    expect(target).toBeNull();
    expect(formatCountdownText(target, nowMs, 'Handoff time reached')).toBe('');
  });

  it('ignores Go zero timestamps and avoids falsely reporting reached', () => {
    const target = resolveHandoffTargetMs(
      {
        schedule_id: 1,
        schedule_name: 'Core SRE Primary',
        remaining_seconds: 0,
        handoff_time: '0001-01-01T00:00:00Z',
      },
      nowMs,
    );
    expect(target).toBeNull();
    expect(formatCountdownText(target, nowMs, 'Handoff time reached')).toBe('');
  });

  it('returns reachedLabel when a valid ISO timestamp is in the past', () => {
    const target = resolveHandoffTargetMs(
      {
        schedule_id: 1,
        schedule_name: 'Core SRE Primary',
        remaining_seconds: 0,
        handoff_time: new Date(nowMs - 5000).toISOString(),
      },
      nowMs,
    );
    expect(target).toBe(nowMs - 5000);
    expect(formatCountdownText(target, nowMs, 'Handoff time reached')).toBe('Handoff time reached');
  });
});

describe('OnCallPage integration', () => {
  beforeEach(() => {
    vi.resetAllMocks();
    localStorage.setItem('opspilot-locale', 'zh-CN');
    vi.mocked(oncallApi.listSchedules).mockResolvedValue({ items: [schedule], total: 1 });
    vi.mocked(oncallApi.listPendingOverrides).mockResolvedValue({ items: [], total: 0 });
    vi.mocked(oncallApi.listEscalationPolicies).mockResolvedValue({ items: [], total: 0 });
    vi.mocked(oncallApi.getCalendarShifts).mockResolvedValue({
      shifts: [primarySlot, secondarySlot],
      gaps: [],
      start: '2026-11-01T00:00:00Z',
      end: '2026-12-01T00:00:00Z',
    });
    vi.mocked(oncallApi.getCurrentOnCall).mockResolvedValue({
      schedule_id: 1,
      schedule_name: 'Core SRE Primary',
      primary_user: primarySlot,
      secondary_user: secondarySlot,
      remaining_seconds: 7265,
      handoff_time: '09:30',
    });
    vi.mocked(oncallApi.listRoutes).mockResolvedValue({ items: [route], total: 1 });
    vi.mocked(usersApi.listUsers).mockResolvedValue({
      items: [
        {
          id: 1,
          email: 'alice@example.com',
          display_name: 'Alice',
          phone: '13800000001',
          role: 'admin',
          status: 'active',
          created_at: '2026-11-01T00:00:00Z',
          updated_at: '2026-11-01T00:00:00Z',
        },
        {
          id: 2,
          email: 'bob@example.com',
          display_name: 'Bob',
          phone: '13800000002',
          role: 'user',
          status: 'active',
          created_at: '2026-11-01T00:00:00Z',
          updated_at: '2026-11-01T00:00:00Z',
        },
      ],
      total: 2,
    });
  });

  afterEach(() => {
    cleanup();
  });

  it('renders valid fallback countdown without NaN and clean Chinese labels without mixed English', async () => {
    const { container } = render(
      <MemoryRouter initialEntries={['/oncall']}>
        <OnCallPage />
      </MemoryRouter>,
    );

    const countdownEl = await screen.findByText(/02:01:0[0-5]/);
    expect(countdownEl).toBeInTheDocument();
    expect(container.textContent).not.toContain('NaN');

    expect(screen.getByText('一线主值班人')).toBeInTheDocument();
    expect(screen.getByText('二线专家支持')).toBeInTheDocument();
    expect(container.textContent).not.toContain('(Primary)');
    expect(container.textContent).not.toContain('(Secondary)');
  });

  it('lazily loads and renders RouteTreeTab when switching to the routes tab', async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter initialEntries={['/oncall']}>
        <OnCallPage />
      </MemoryRouter>,
    );

    await screen.findByText('一线主值班人');
    await user.click(screen.getByRole('tab', { name: /告警路由/ }));

    expect(await screen.findByText('Payment P1 Route')).toBeInTheDocument();
    expect(screen.getByText('告警多维标签路由树')).toBeInTheDocument();
  });

  it('shows an inline retry alert when loading schedules fails and recovers on retry', async () => {
    const user = userEvent.setup();
    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
    vi.mocked(oncallApi.listSchedules)
      .mockRejectedValueOnce(new Error('Service temporarily unavailable'))
      .mockResolvedValue({ items: [schedule], total: 1 });

    try {
      render(
        <MemoryRouter initialEntries={['/oncall']}>
          <OnCallPage />
        </MemoryRouter>,
      );

      const alert = await screen.findByRole('alert');
      expect(alert).toHaveTextContent('Service temporarily unavailable');

      await user.click(screen.getByRole('button', { name: '重试' }));
      expect(await screen.findByText('一线主值班人')).toBeInTheDocument();
      expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    } finally {
      consoleSpy.mockRestore();
    }
  });
});
