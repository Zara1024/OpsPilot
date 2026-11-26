import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, expect, it, vi } from 'vitest';
import { RouteTreeTab } from './RouteTreeTab';
import * as oncallApi from '@/api/oncall';

vi.mock('@/api/oncall', async () => {
  const actual = await vi.importActual<typeof import('@/api/oncall')>('@/api/oncall');
  return {
    ...actual,
    listRoutes: vi.fn(),
    deleteRoute: vi.fn(),
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

const route = {
  id: 42,
  team_id: 1,
  name: 'Payment P1 Route',
  matcher_json: '[]',
  matchers: [{ name: 'service', operator: '=' as const, value: 'payment' }],
  priority: 10,
  schedule_id: 1,
  group_wait_seconds: 30,
  group_interval_seconds: 300,
  enabled: true,
  created_at: '2026-11-01T00:00:00Z',
  updated_at: '2026-11-01T00:00:00Z',
};

beforeEach(() => {
  vi.resetAllMocks();
  localStorage.setItem('opspilot-locale', 'en-US');
  vi.mocked(oncallApi.listRoutes).mockResolvedValue({ items: [route], total: 1 });
  vi.mocked(oncallApi.deleteRoute).mockResolvedValue({ ok: true });
});

it('uses accessible useDialogs confirmation instead of window.confirm when deleting a route', async () => {
  const user = userEvent.setup();
  const confirmSpy = vi.spyOn(window, 'confirm').mockImplementation(() => true);

  render(
    <MemoryRouter>
      <RouteTreeTab schedules={[schedule]} escalationPolicies={[]} />
    </MemoryRouter>,
  );
  await screen.findByText('Payment P1 Route');

  await user.click(screen.getByRole('button', { name: 'Delete' }));
  expect(confirmSpy).not.toHaveBeenCalled();

  const dialog = await screen.findByRole('dialog');
  expect(dialog).toHaveTextContent('Are you sure you want to delete this route?');

  await user.click(screen.getByRole('button', { name: 'Cancel' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  expect(oncallApi.deleteRoute).not.toHaveBeenCalled();

  await user.click(screen.getByRole('button', { name: 'Delete' }));
  await screen.findByRole('dialog');
  await user.click(screen.getByRole('button', { name: 'Confirm' }));

  await waitFor(() => expect(oncallApi.deleteRoute).toHaveBeenCalledWith(42));
  expect(confirmSpy).not.toHaveBeenCalled();
});
