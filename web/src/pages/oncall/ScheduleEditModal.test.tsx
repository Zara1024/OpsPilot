import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import * as oncallApi from '@/api/oncall';
import { ScheduleEditModal } from './ScheduleEditModal';

vi.mock('@/api/oncall', async () => {
  const actual = await vi.importActual<typeof import('@/api/oncall')>('@/api/oncall');
  return {
    ...actual,
    createSchedule: vi.fn(),
    updateSchedule: vi.fn(),
    deleteSchedule: vi.fn(),
  };
});

describe('ScheduleEditModal', () => {
  beforeEach(() => {
    vi.resetAllMocks();
    localStorage.setItem('opspilot-locale', 'zh-CN');
  });

  afterEach(() => {
    cleanup();
  });

  it('hydrates and saves escalation chain repeat count and interval', async () => {
    const user = userEvent.setup();
    vi.mocked(oncallApi.updateSchedule).mockResolvedValue({
      id: 9,
      team_id: 1,
      name: 'Core SRE',
      description: 'Primary team',
      timezone: 'Asia/Shanghai',
      handoff_time: '09:00:00',
      reminder_advance_hours: 3,
      escalation_repeat_count: 3,
      escalation_repeat_interval_seconds: 180,
      require_swap_approval: false,
      enabled: true,
      created_by: 1,
      created_at: '2026-11-26T00:00:00Z',
      updated_at: '2026-11-26T00:00:00Z',
    });

    const onSuccess = vi.fn();
    const onClose = vi.fn();

    render(
      <ScheduleEditModal
        open
        mode="edit"
        onClose={onClose}
        onSuccess={onSuccess}
        users={[
          {
            id: 1,
            email: 'alice@opspilot.local',
            display_name: 'Alice',
            phone: '13800000001',
            role: 'admin',
            status: 'active',
            created_at: '2026-11-01T00:00:00Z',
            updated_at: '2026-11-01T00:00:00Z',
          },
        ]}
        initialSchedule={{
          id: 9,
          team_id: 1,
          name: 'Core SRE',
          description: 'Primary team',
          timezone: 'Asia/Shanghai',
          handoff_time: '09:00:00',
          reminder_advance_hours: 3,
          escalation_repeat_count: 2,
          escalation_repeat_interval_seconds: 240,
          require_swap_approval: false,
          enabled: true,
          created_by: 1,
          created_at: '2026-11-26T00:00:00Z',
          updated_at: '2026-11-26T00:00:00Z',
          rotations: [
            {
              id: 11,
              name: '一线值班轮转',
              tier: 1,
              rotation_type: 'daily',
              shift_length_seconds: 86400,
              users: [1],
              effective_from: '2026-11-25T00:00:00Z',
              time_restriction_type: 'none',
            },
          ],
        }}
      />
    );

    const repeatCountInput = screen.getByLabelText('升级链循环次数 (0-5 轮)') as HTMLInputElement;
    const repeatIntervalInput = screen.getByLabelText('循环重试间隔 (秒)') as HTMLInputElement;

    expect(repeatCountInput.value).toBe('2');
    expect(repeatIntervalInput.value).toBe('240');

    await user.clear(repeatCountInput);
    await user.type(repeatCountInput, '3');
    await user.clear(repeatIntervalInput);
    await user.type(repeatIntervalInput, '180');

    await user.click(screen.getByRole('button', { name: '保存计划' }));

    await waitFor(() => {
      expect(oncallApi.updateSchedule).toHaveBeenCalledTimes(1);
    });
    expect(vi.mocked(oncallApi.updateSchedule).mock.calls[0][1]).toMatchObject({
      escalation_repeat_count: 3,
      escalation_repeat_interval_seconds: 180,
    });
  });
});
