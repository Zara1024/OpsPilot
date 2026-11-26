import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import * as alertsApi from '@/api/alerts';
import AlertIntegrationsPage from './AlertIntegrations';

let mockPermissions = {
  role: 'admin',
  isAdmin: true,
  isUser: false,
  isViewer: false,
  canMutate: true,
};

vi.mock('@/store/me', () => ({
  usePermissions: () => mockPermissions,
}));

vi.mock('@/api/alerts', async () => {
  const actual = await vi.importActual<typeof import('@/api/alerts')>('@/api/alerts');
  return {
    ...actual,
    listIngestSources: vi.fn(),
    createIngestSource: vi.fn(),
    updateIngestSource: vi.fn(),
    deleteIngestSource: vi.fn(),
    rotateIngestSourceToken: vi.fn(),
    testIngestSource: vi.fn(),
    listHeartbeats: vi.fn(),
    createHeartbeat: vi.fn(),
    updateHeartbeat: vi.fn(),
    deleteHeartbeat: vi.fn(),
    rotateHeartbeatToken: vi.fn(),
    manualPingHeartbeat: vi.fn(),
  };
});

const zabbixSource: alertsApi.IngestSource = {
  id: 11,
  name: 'Zabbix 生产监控',
  description: '核心机房 Zabbix 7',
  provider: 'zabbix',
  webhook_url: '/api/v1/alerts/ingest/zabbix/zbx-token-001',
  token: 'zbx-token-001',
  signing_secret_configured: true,
  default_severity: 'warning',
  default_labels: { env: 'prod', service: 'gateway' },
  auto_resolve: true,
  enabled: true,
  rate_limit_per_min: 120,
  received_count: 8,
  error_count: 0,
  last_received_at: '2026-11-26T08:00:00Z',
  last_status: 'processed',
  last_error: '',
  last_payload_preview: '{"trigger_id":"44102"}',
  created_at: '2026-11-26T07:00:00Z',
  updated_at: '2026-11-26T08:00:00Z',
};

const huaweiSource: alertsApi.IngestSource = {
  id: 12,
  name: '华为云 CES',
  description: '华北四区 RDS/ECS',
  provider: 'huawei_ces',
  webhook_url: '/api/v1/alerts/ingest/huawei_ces/hw-token-002',
  token: 'hw-token-002',
  signing_secret_configured: false,
  default_severity: 'critical',
  default_labels: { cloud: 'huawei' },
  auto_resolve: true,
  enabled: true,
  rate_limit_per_min: 120,
  received_count: 1,
  error_count: 0,
  last_received_at: '2026-11-26T08:05:00Z',
  last_status: 'subscription_confirmation',
  last_error: '',
  subscribe_url: 'https://smn.cn-north-4.myhuaweicloud.com/confirm?token=demo',
  created_at: '2026-11-26T07:00:00Z',
  updated_at: '2026-11-26T08:05:00Z',
};

const sampleHeartbeat: alertsApi.Heartbeat = {
  id: 3,
  name: 'Prometheus Watchdog',
  description: '监控自身保活心跳',
  ping_url: '/api/v1/alerts/heartbeat/hb-token-003',
  token: 'hb-token-003',
  interval_seconds: 60,
  grace_seconds: 120,
  severity: 'critical',
  labels: { service: 'monitoring' },
  enabled: true,
  state: 'waiting',
  last_ping_ip: '',
  last_ping_message: '',
  ping_count: 0,
  created_at: '2026-11-26T07:00:00Z',
  updated_at: '2026-11-26T07:00:00Z',
};

describe('AlertIntegrationsPage', () => {
  beforeEach(() => {
    vi.resetAllMocks();
    localStorage.setItem('opspilot-locale', 'zh-CN');
    mockPermissions = {
      role: 'admin',
      isAdmin: true,
      isUser: false,
      isViewer: false,
      canMutate: true,
    };
  });

  afterEach(() => {
    cleanup();
  });

  it('renders ingest sources, opens Zabbix/Huawei guides, simulates alert, and rotates token', async () => {
    const user = userEvent.setup();
    vi.mocked(alertsApi.listIngestSources).mockResolvedValue({
      items: [zabbixSource, huaweiSource],
      total: 2,
    });
    vi.mocked(alertsApi.listHeartbeats).mockResolvedValue({
      items: [sampleHeartbeat],
      total: 1,
    });
    vi.mocked(alertsApi.testIngestSource).mockResolvedValue({
      source_id: 11,
      provider: 'zabbix',
      status: 'processed',
      fired_count: 1,
      resolved_count: 0,
      ignored_count: 0,
      incident_ids: [91],
      message: '已处理 1 条触发、0 条恢复、0 条忽略',
    });
    vi.mocked(alertsApi.rotateIngestSourceToken).mockResolvedValue({
      ...zabbixSource,
      token: 'zbx-token-rotated',
    });

    render(
      <MemoryRouter>
        <AlertIntegrationsPage />
      </MemoryRouter>,
    );

    expect(await screen.findByText('Zabbix 生产监控')).toBeInTheDocument();
    expect(screen.getByText('华为云 CES')).toBeInTheDocument();
    expect(screen.getByText('待确认 SMN 订阅')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: '前往 On-Call 路由' })).toHaveAttribute('href', '/oncall?tab=routes');

    // Open Zabbix integration guide & verify JS webhook snippet
    const guideBtns = screen.getAllByRole('button', { name: '接入向导' });
    await user.click(guideBtns[0]);
    expect(await screen.findByText('Zabbix 生产监控 — 接入配置向导')).toBeInTheDocument();
    expect(screen.getByDisplayValue(/Zabbix\.?|HttpRequest/)).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: '完成' }));

    // Simulate firing alert
    const testFireBtns = screen.getAllByRole('button', { name: '模拟告警' });
    await user.click(testFireBtns[0]);
    await waitFor(() => {
      expect(alertsApi.testIngestSource).toHaveBeenCalledWith(11, 'fire');
    });
    expect(await screen.findByText(/模拟触发完成/)).toBeInTheDocument();

    // Rotate token via useDialogs confirm
    const rotateBtns = screen.getAllByRole('button', { name: '轮换 Token' });
    await user.click(rotateBtns[0]);
    await user.click(await screen.findByRole('button', { name: '确认' }));
    await waitFor(() => {
      expect(alertsApi.rotateIngestSourceToken).toHaveBeenCalledWith(11);
    });
  }, 15000);

  it('creates an ingest source and switches to Heartbeats tab to ping manually', async () => {
    const user = userEvent.setup();
    vi.mocked(alertsApi.listIngestSources).mockResolvedValue({ items: [], total: 0 });
    vi.mocked(alertsApi.listHeartbeats).mockResolvedValue({
      items: [sampleHeartbeat],
      total: 1,
    });
    vi.mocked(alertsApi.createIngestSource).mockResolvedValue(zabbixSource);
    vi.mocked(alertsApi.manualPingHeartbeat).mockResolvedValue({
      heartbeat: { ...sampleHeartbeat, state: 'healthy', ping_count: 1 },
      previous_state: 'waiting',
      auto_resolved: false,
      message: 'heartbeat ok',
    });

    render(
      <MemoryRouter>
        <AlertIntegrationsPage />
      </MemoryRouter>,
    );

    expect(await screen.findByText('尚未配置外部告警接入源')).toBeInTheDocument();

    await user.click(screen.getAllByRole('button', { name: '新建接入源' })[0]);
    await user.type(screen.getByLabelText(/接入源名称/), 'Alertmanager 主集群');
    await user.click(screen.getByRole('button', { name: '保存' }));
    await waitFor(() => {
      expect(alertsApi.createIngestSource).toHaveBeenCalledTimes(1);
    });

    // Switch to Heartbeats tab & ping manually
    await user.click(screen.getByRole('tab', { name: /死信心跳监控/ }));
    expect(await screen.findByText('Prometheus Watchdog')).toBeInTheDocument();
    expect(screen.getByText('等待首次心跳')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '手动 Ping' }));
    await waitFor(() => {
      expect(alertsApi.manualPingHeartbeat).toHaveBeenCalledWith(3);
    });
    expect(await screen.findByText(/正常监听中/)).toBeInTheDocument();
  }, 15000);
});
