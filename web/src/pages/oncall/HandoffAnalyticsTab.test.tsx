import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import * as oncallApi from '@/api/oncall';
import { HandoffAnalyticsTab } from './HandoffAnalyticsTab';

vi.mock('@/api/oncall', async () => {
  const actual = await vi.importActual<typeof import('@/api/oncall')>('@/api/oncall');
  return {
    ...actual,
    getCurrentHandoff: vi.fn(),
    listHandoffLogs: vi.fn(),
    ackHandoff: vi.fn(),
    sendHandoffReminderNow: vi.fn(),
    getBurnoutAnalytics: vi.fn(),
    getNoisyAlerts: vi.fn(),
  };
});

describe('HandoffAnalyticsTab', () => {
  beforeEach(() => {
    vi.resetAllMocks();
    localStorage.setItem('opspilot-locale', 'zh-CN');

    vi.mocked(oncallApi.getCurrentHandoff).mockResolvedValue({
      schedule_id: 1,
      schedule_name: '核心支付值班',
      outgoing_user_id: 1,
      outgoing_user_name: 'Alice',
      incoming_user_id: 2,
      incoming_user_name: 'Bob',
      shift_start: '2026-11-25T01:00:00Z',
      shift_end: '2026-11-26T01:00:00Z',
      next_handoff_time: '09:00:00',
      time_remaining: '02:00:00',
      status: 'pending',
      notes: '核心链路稳定',
      active_incidents_summary: [],
      pending_checklist: [],
    });
    vi.mocked(oncallApi.getBurnoutAnalytics).mockResolvedValue({
      mtta_seconds: 45,
      mtta_rating: 'good',
      night_call_count: 1,
      total_incidents: 4,
      burnout_index: 22,
      noise_reduction_ratio: 68,
      engineer_loads: [],
      trends: [],
    });
    vi.mocked(oncallApi.getNoisyAlerts).mockResolvedValue({ items: [], total: 0 });
  });

  afterEach(() => {
    cleanup();
  });

  it('renders handoff logs and triggers manual reminder with feedback', async () => {
    const user = userEvent.setup();
    vi.mocked(oncallApi.listHandoffLogs).mockResolvedValue({
      items: [
        {
          id: 11,
          schedule_id: 1,
          schedule_name: '核心支付值班',
          user_id: 2,
          user_name: 'Bob',
          previous_user_id: 1,
          previous_user_name: 'Alice',
          shift_start: '2026-11-25T01:00:00Z',
          shift_end: '2026-11-26T01:00:00Z',
          reminder_type: 'pre_shift_reminder',
          notes: '已通知接班人 Bob',
          ack_status: 'pending',
          channel_type: 'feishu',
          status: 'sent',
          created_at: '2026-11-25T06:00:00Z',
        },
      ],
      total: 1,
    });
    vi.mocked(oncallApi.sendHandoffReminderNow).mockResolvedValue({
      ok: true,
      status: 'sent',
      channel_type: 'feishu',
    });

    render(<HandoffAnalyticsTab schedules={[]} />);

    expect(await screen.findByText('交接签收与接班提醒记录')).toBeInTheDocument();
    expect(screen.getByText('班前自动提醒')).toBeInTheDocument();
    expect(screen.getByText('已通知接班人 Bob')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '立即发送接班提醒' }));

    await waitFor(() => {
      expect(oncallApi.sendHandoffReminderNow).toHaveBeenCalledWith(1);
    });
    expect(await screen.findByText('接班提醒已通过 feishu 发送给 Bob')).toBeInTheDocument();
  });

  it('renders empty state when there are no handoff or reminder logs', async () => {
    vi.mocked(oncallApi.listHandoffLogs).mockResolvedValue({ items: [], total: 0 });

    render(<HandoffAnalyticsTab schedules={[]} />);

    expect(await screen.findByText('暂无交接签收与提醒记录')).toBeInTheDocument();
  });
});
