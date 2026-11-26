import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import * as oncallApi from '@/api/oncall';
import { ChannelGatewaysSection } from './ChannelGatewaysSection';

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

vi.mock('@/api/oncall', async () => {
  const actual = await vi.importActual<typeof import('@/api/oncall')>('@/api/oncall');
  return {
    ...actual,
    getChannelConfig: vi.fn(),
    updateChannelConfig: vi.fn(),
    testSMSChannel: vi.fn(),
    testEmailChannel: vi.fn(),
  };
});

describe('ChannelGatewaysSection', () => {
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

  it('keeps stored secrets blank and saves multi-channel configuration cleanly', async () => {
    const user = userEvent.setup();
    vi.mocked(oncallApi.getChannelConfig).mockResolvedValue({
      sms_enabled: true,
      sms_provider: 'aliyun',
      aliyun_access_key_id: 'LTAI5Sms',
      aliyun_secret_configured: true,
      email_enabled: true,
      smtp_host: 'smtp.example.com',
      smtp_port: 465,
      smtp_tls_mode: 'ssl',
      smtp_timeout_seconds: 10,
      smtp_password_configured: true,
      war_room_provider: 'feishu',
      feishu_app_id: 'cli_123',
      feishu_app_secret_configured: true,
      callback_urls: {
        feishu: 'https://opspilot.test/api/v1/oncall/chatops/callback/feishu?token=tok1',
        dingtalk: 'https://opspilot.test/api/v1/oncall/chatops/callback/dingtalk?token=tok1',
      },
    });
    vi.mocked(oncallApi.updateChannelConfig).mockResolvedValue({
      ok: true,
      config: {
        sms_enabled: true,
        sms_provider: 'aliyun',
        email_enabled: true,
        smtp_port: 465,
        smtp_tls_mode: 'ssl',
        smtp_timeout_seconds: 10,
        war_room_provider: 'feishu',
        aliyun_secret_configured: true,
        smtp_password_configured: true,
        feishu_app_secret_configured: true,
      },
    });

    render(<ChannelGatewaysSection />);

    expect(await screen.findByDisplayValue('https://opspilot.test/api/v1/oncall/chatops/callback/feishu?token=tok1')).toBeInTheDocument();
    const secretInputs = screen.getAllByPlaceholderText('已配置密钥，留空保持不变') as HTMLInputElement[];
    expect(secretInputs.length).toBeGreaterThanOrEqual(3);
    secretInputs.forEach((input) => expect(input.value).toBe(''));

    await user.click(screen.getByRole('button', { name: '保存多通道配置' }));

    await waitFor(() => {
      expect(oncallApi.updateChannelConfig).toHaveBeenCalledTimes(1);
    });
    expect(vi.mocked(oncallApi.updateChannelConfig).mock.calls[0][0]).toMatchObject({
      sms_enabled: true,
      sms_provider: 'aliyun',
      aliyun_access_key_secret: '',
      smtp_password: '',
      war_room_provider: 'feishu',
    });
  });

  it('triggers live SMS and SMTP email test buttons and renders feedback', async () => {
    const user = userEvent.setup();
    vi.mocked(oncallApi.getChannelConfig).mockResolvedValue({
      sms_enabled: true,
      sms_provider: 'tencent',
      email_enabled: true,
      smtp_host: 'smtp.example.com',
      smtp_port: 465,
      smtp_tls_mode: 'ssl',
      smtp_timeout_seconds: 10,
      war_room_provider: '',
    });
    vi.mocked(oncallApi.testSMSChannel).mockResolvedValue({
      provider: 'tencent',
      status: 'sent',
      phone_number: '13800000001',
      biz_id: 'ser_001',
      message: '已通过腾讯云短信发送成功 (SerialNo: ser_001)',
    });
    vi.mocked(oncallApi.testEmailChannel).mockResolvedValue({
      provider: 'smtp',
      status: 'sent',
      to: 'admin@opspilot.local',
      subject: 'test',
      message: '邮件已通过 SMTP 成功发送',
    });

    render(<ChannelGatewaysSection />);

    await screen.findByText('多通道通知与协同网关');
    await user.click(screen.getByRole('button', { name: '发送测试短信' }));
    expect(await screen.findByText('已通过腾讯云短信发送成功 (SerialNo: ser_001)')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '发送测试邮件' }));
    expect(await screen.findByText('邮件已通过 SMTP 成功发送')).toBeInTheDocument();
  });

  it('shows read-only warning and disables mutating actions for non-admin users', async () => {
    mockPermissions = {
      role: 'user',
      isAdmin: false,
      isUser: true,
      isViewer: false,
      canMutate: true,
    };
    vi.mocked(oncallApi.getChannelConfig).mockResolvedValue({
      sms_enabled: false,
      sms_provider: '',
      email_enabled: false,
      smtp_port: 465,
      smtp_tls_mode: 'ssl',
      smtp_timeout_seconds: 10,
      war_room_provider: '',
    });

    render(<ChannelGatewaysSection />);

    expect(
      await screen.findByText('当前账号为只读权限，修改短信、邮件或作战室密钥与实网测试需要管理员权限。')
    ).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '保存多通道配置' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: '发送测试短信' })).toBeDisabled();
    expect(screen.getByRole('button', { name: '发送测试邮件' })).toBeDisabled();
  });
});
