import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import * as oncallApi from '@/api/oncall';
import { VoiceGatewayTab } from './VoiceGatewayTab';

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
    getVoiceConfig: vi.fn(),
    updateVoiceConfig: vi.fn(),
    testVoiceCall: vi.fn(),
    rotateVoiceCallbackToken: vi.fn(),
  };
});

describe('VoiceGatewayTab', () => {
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

  it('shows failure chip and real error message without fake DTMF ack when test call fails', async () => {
    const user = userEvent.setup();
    vi.mocked(oncallApi.getVoiceConfig).mockResolvedValue({
      provider: 'aliyun',
      dispatch_enabled: true,
      aliyun_access_key_id: 'LTAI5tTestKey',
      aliyun_secret_configured: true,
      aliyun_called_show_number: '057188888888',
      aliyun_tts_code: 'TTS_123456',
      aliyun_region: 'cn-hangzhou',
      callback_urls: {
        aliyun: 'https://opspilot.test/api/v1/oncall/voice/callback/aliyun?token=tok1',
      },
    });
    vi.mocked(oncallApi.testVoiceCall).mockResolvedValue({
      call_id: 'call_fail_1',
      provider: 'aliyun',
      status: 'failed',
      phone_number: '13800000001',
      tts_content: '',
      duration_secs: 0,
      message: '阿里云外呼失败: [isp.RAM_PERMISSION_DENY] 权限不足',
    });

    const { container } = render(<VoiceGatewayTab />);

    expect(await screen.findByText('已加密存储')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('已配置密钥，留空保持不变')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '发起在线语音外呼测试' }));

    expect(await screen.findByText('呼叫失败')).toBeInTheDocument();
    expect(screen.getByText('阿里云外呼失败: [isp.RAM_PERMISSION_DENY] 权限不足')).toBeInTheDocument();
    expect(container.textContent).not.toContain('已确认认领接单');
    expect(screen.queryByText(/DTMF 按键结果/)).not.toBeInTheDocument();
  });

  it('keeps secret input blank on load and sends empty string to preserve stored secret on save', async () => {
    const user = userEvent.setup();
    vi.mocked(oncallApi.getVoiceConfig).mockResolvedValue({
      provider: 'aliyun',
      dispatch_enabled: true,
      aliyun_access_key_id: 'LTAI5tTestKey',
      aliyun_secret_configured: true,
      aliyun_region: 'cn-hangzhou',
    });
    vi.mocked(oncallApi.updateVoiceConfig).mockResolvedValue({
      ok: true,
      config: {
        provider: 'aliyun',
        dispatch_enabled: true,
        aliyun_access_key_id: 'LTAI5tTestKey',
        aliyun_secret_configured: true,
        aliyun_region: 'cn-hangzhou',
      },
    });

    render(<VoiceGatewayTab />);

    const secretInput = (await screen.findByPlaceholderText('已配置密钥，留空保持不变')) as HTMLInputElement;
    expect(secretInput.value).toBe('');

    await user.click(screen.getByRole('button', { name: '保存网关配置' }));

    await waitFor(() => {
      expect(oncallApi.updateVoiceConfig).toHaveBeenCalledTimes(1);
    });
    const sentPayload = vi.mocked(oncallApi.updateVoiceConfig).mock.calls[0][0];
    expect(sentPayload.aliyun_access_key_secret).toBe('');
  });

  it('renders read-only notice and disables mutating actions for non-admin users', async () => {
    mockPermissions = {
      role: 'user',
      isAdmin: false,
      isUser: true,
      isViewer: false,
      canMutate: true,
    };
    vi.mocked(oncallApi.getVoiceConfig).mockResolvedValue({
      provider: 'mock',
      dispatch_enabled: true,
    });

    render(<VoiceGatewayTab />);

    expect(
      await screen.findByText('当前账号为只读权限，修改云厂商密钥配置及发起实网外呼需要管理员权限。'),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '保存网关配置' })).toBeDisabled();
    expect(screen.getByRole('button', { name: '发起在线语音外呼测试' })).toBeDisabled();
  });

  it('renders backup provider credentials, saves failover settings, and shows failover banner', async () => {
    const user = userEvent.setup();
    vi.mocked(oncallApi.getVoiceConfig).mockResolvedValue({
      provider: 'aliyun',
      fallback_provider: 'tencent',
      failover_timeout_seconds: 12,
      dispatch_enabled: true,
      aliyun_access_key_id: 'LTAI5tTestKey',
      aliyun_secret_configured: true,
      tencent_secret_id: 'AKIDTest',
      tencent_secret_configured: false,
    });
    vi.mocked(oncallApi.updateVoiceConfig).mockResolvedValue({
      ok: true,
      config: {
        provider: 'aliyun',
        fallback_provider: 'tencent',
        failover_timeout_seconds: 12,
        dispatch_enabled: true,
        aliyun_access_key_id: 'LTAI5tTestKey',
        aliyun_secret_configured: true,
        tencent_secret_id: 'AKIDTest',
      },
    });
    vi.mocked(oncallApi.testVoiceCall).mockResolvedValue({
      call_id: 'call_failover_1',
      provider: 'tencent',
      fallback_from: 'aliyun',
      primary_error: 'aliyun upstream 500',
      status: 'calling',
      out_call_id: 'tx_123',
      phone_number: '13800000001',
      tts_content: 'Core DB down',
      duration_secs: 0,
      message: '已切换腾讯云呼叫',
    });

    render(<VoiceGatewayTab />);

    expect(await screen.findByText('备援通道：腾讯云语音参数')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '保存网关配置' }));
    await waitFor(() => {
      expect(oncallApi.updateVoiceConfig).toHaveBeenCalledTimes(1);
    });
    expect(vi.mocked(oncallApi.updateVoiceConfig).mock.calls[0][0]).toMatchObject({
      provider: 'aliyun',
      fallback_provider: 'tencent',
      failover_timeout_seconds: 12,
    });

    await user.click(screen.getByRole('button', { name: '发起在线语音外呼测试' }));
    expect(
      await screen.findByText('主通道（aliyun）外呼异常，已自动切换至备援通道（tencent）'),
    ).toBeInTheDocument();
    expect(screen.getByText(/aliyun upstream 500/)).toBeInTheDocument();
  });

  it('rotates voice webhook callback token after confirmation and updates URLs', async () => {
    const user = userEvent.setup();
    vi.mocked(oncallApi.getVoiceConfig).mockResolvedValue({
      provider: 'aliyun',
      dispatch_enabled: true,
      aliyun_secret_configured: true,
      callback_urls: {
        aliyun: 'https://opspilot.test/api/v1/oncall/voice/callback/aliyun?token=oldtok',
      },
    });
    vi.mocked(oncallApi.rotateVoiceCallbackToken).mockResolvedValue({
      ok: true,
      config: {
        provider: 'aliyun',
        dispatch_enabled: true,
        aliyun_secret_configured: true,
        callback_urls: {
          aliyun: 'https://opspilot.test/api/v1/oncall/voice/callback/aliyun?token=newtok999',
        },
      },
    });

    render(<VoiceGatewayTab />);

    expect(await screen.findByDisplayValue('https://opspilot.test/api/v1/oncall/voice/callback/aliyun?token=oldtok')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '一键轮换回调 Token' }));
    await user.click(await screen.findByRole('button', { name: '确认' }));

    await waitFor(() => {
      expect(oncallApi.rotateVoiceCallbackToken).toHaveBeenCalledTimes(1);
    });
    expect(await screen.findByDisplayValue('https://opspilot.test/api/v1/oncall/voice/callback/aliyun?token=newtok999')).toBeInTheDocument();
    expect(screen.getByText(/语音回调 Token 已轮换生效/)).toBeInTheDocument();
  });
});
