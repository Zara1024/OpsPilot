import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import * as oncallApi from '@/api/oncall';
import { MaintenanceWindowsSection } from './MaintenanceWindowsSection';

vi.mock('@/api/oncall', async () => {
  const actual = await vi.importActual<typeof import('@/api/oncall')>('@/api/oncall');
  return {
    ...actual,
    listMaintenanceWindows: vi.fn(),
    createMaintenanceWindow: vi.fn(),
    cancelMaintenanceWindow: vi.fn(),
    deleteMaintenanceWindow: vi.fn(),
  };
});

const sampleSchedule: oncallApi.OnCallSchedule = {
  id: 1,
  team_id: 0,
  name: '核心支付值班',
  description: '',
  timezone: 'Asia/Shanghai',
  handoff_time: '09:00:00',
  reminder_advance_hours: 3,
  require_swap_approval: false,
  enabled: true,
  created_by: 1,
  created_at: '',
  updated_at: '',
};

const sampleWindow: oncallApi.MaintenanceWindowItem = {
  id: 5,
  name: '支付核心库凌晨迁移',
  description: '主从切换窗口',
  schedule_id: 1,
  schedule_name: '核心支付值班',
  matcher_json: '[{"name":"service","operator":"=","value":"payment"}]',
  matchers: [{ name: 'service', operator: '=', value: 'payment' }],
  action: 'suppress_all',
  starts_at: '2026-11-26T00:00:00Z',
  ends_at: '2026-11-26T04:00:00Z',
  status: 'active',
  runtime_status: 'running',
  suppressed_count: 3,
  created_by: 1,
  created_at: '2026-11-26T00:00:00Z',
  updated_at: '2026-11-26T00:00:00Z',
};

describe('MaintenanceWindowsSection', () => {
  beforeEach(() => {
    vi.resetAllMocks();
    localStorage.setItem('opspilot-locale', 'zh-CN');
  });

  afterEach(() => {
    cleanup();
  });

  it('lists active windows and cancels one with confirmation', async () => {
    const user = userEvent.setup();
    vi.mocked(oncallApi.listMaintenanceWindows).mockResolvedValue({
      items: [sampleWindow],
      total: 1,
    });
    vi.mocked(oncallApi.cancelMaintenanceWindow).mockResolvedValue({
      ok: true,
      item: { ...sampleWindow, status: 'cancelled', runtime_status: 'cancelled' },
    });

    render(<MaintenanceWindowsSection schedules={[sampleSchedule]} />);

    expect(await screen.findByText('支付核心库凌晨迁移')).toBeInTheDocument();
    expect(screen.getByText('已抑制 3 次')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '终止' }));
    await user.click(await screen.findByRole('button', { name: '确认' }));

    await waitFor(() => {
      expect(oncallApi.cancelMaintenanceWindow).toHaveBeenCalledWith(5);
    });
  }, 15000);

  it('creates a new maintenance window from the modal', async () => {
    const user = userEvent.setup();
    vi.mocked(oncallApi.listMaintenanceWindows).mockResolvedValue({ items: [], total: 0 });
    vi.mocked(oncallApi.createMaintenanceWindow).mockResolvedValue({
      ...sampleWindow,
      id: 6,
      name: '灰度发布静默窗',
      suppressed_count: 0,
    });

    render(<MaintenanceWindowsSection schedules={[sampleSchedule]} />);

    expect(await screen.findByText('暂无计划内维护窗口')).toBeInTheDocument();

    await user.click(screen.getAllByRole('button', { name: '新建维护窗口' })[0]);
    await user.type(screen.getByPlaceholderText('如：核心支付库凌晨升配维护'), '灰度发布静默窗');
    await user.click(screen.getByRole('button', { name: '确认创建' }));

    await waitFor(() => {
      expect(oncallApi.createMaintenanceWindow).toHaveBeenCalledTimes(1);
    });
    expect(vi.mocked(oncallApi.createMaintenanceWindow).mock.calls[0][0]).toMatchObject({
      name: '灰度发布静默窗',
      schedule_id: 0,
      action: 'suppress_all',
    });
  }, 15000);
});
