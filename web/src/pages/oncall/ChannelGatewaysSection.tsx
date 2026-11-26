import { useState, useEffect, useCallback } from 'react';
import {
  MessageSquare,
  Mail,
  Shield,
  Save,
  RefreshCw,
  Copy,
  Check,
  Send,
  ShieldAlert,
} from 'lucide-react';
import {
  Button,
  Card,
  Chip,
  Input,
  Label,
  Select,
  Switch,
} from '@/components/ui';
import { useDialogs } from '@/components/ui/useDialogs';
import { useI18n } from '@/i18n/locale';
import { usePermissions } from '@/store/me';
import {
  getChannelConfig,
  updateChannelConfig,
  testSMSChannel,
  testEmailChannel,
  type ChannelGatewayConfig,
  type TestSMSResult,
  type TestEmailResult,
} from '@/api/oncall';

interface SecretFieldProps {
  label: string;
  value: string;
  configured?: boolean;
  disabled: boolean;
  onChange: (value: string) => void;
}

function SecretField({ label, value, configured, disabled, onChange }: SecretFieldProps) {
  const { tr } = useI18n();
  return (
    <div>
      <div className="flex items-center justify-between gap-2">
        <Label className="text-xs">{label}</Label>
        {configured && <Chip tone="success" dense>{tr('已加密存储', 'Encrypted')}</Chip>}
      </div>
      <Input
        type="password"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={
          configured
            ? tr('已配置密钥，留空保持不变', 'Configured, leave blank to keep')
            : '••••••••••••'
        }
        disabled={disabled}
        className="mt-1 text-xs font-mono"
      />
    </div>
  );
}

export function ChannelGatewaysSection() {
  const { tr } = useI18n();
  const { isAdmin } = usePermissions();
  const { alertAction, dialog } = useDialogs();

  const [config, setConfig] = useState<ChannelGatewayConfig>({
    sms_enabled: false,
    sms_provider: '',
    aliyun_access_key_id: '',
    aliyun_access_key_secret: '',
    aliyun_secret_configured: false,
    aliyun_sign_name: '',
    aliyun_template_code: '',
    aliyun_region: 'cn-hangzhou',
    tencent_secret_id: '',
    tencent_secret_key: '',
    tencent_secret_configured: false,
    tencent_sdk_app_id: '',
    tencent_sign_name: '',
    tencent_template_id: '',
    tencent_region: 'ap-guangzhou',
    email_enabled: false,
    smtp_host: '',
    smtp_port: 465,
    smtp_username: '',
    smtp_password: '',
    smtp_password_configured: false,
    smtp_from: '',
    smtp_tls_mode: 'ssl',
    smtp_timeout_seconds: 10,
    feishu_verification_token: '',
    feishu_verification_configured: false,
    feishu_encrypt_key: '',
    feishu_encrypt_configured: false,
    dingtalk_app_secret: '',
    dingtalk_secret_configured: false,
    war_room_provider: '',
    feishu_app_id: '',
    feishu_app_secret: '',
    feishu_app_secret_configured: false,
    war_room_webhook_url: '',
    war_room_webhook_secret: '',
    war_room_webhook_secret_configured: false,
    callback_urls: {},
  });

  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [saveSuccess, setSaveSuccess] = useState(false);
  const [copiedKey, setCopiedKey] = useState<string | null>(null);

  const [testPhone, setTestPhone] = useState('13800000001');
  const [testingSMS, setTestingSMS] = useState(false);
  const [smsResult, setSmsResult] = useState<TestSMSResult | null>(null);

  const [testEmail, setTestEmail] = useState('');
  const [testingEmail, setTestingEmail] = useState(false);
  const [emailResult, setEmailResult] = useState<TestEmailResult | null>(null);

  const applySanitizedResponse = (res: ChannelGatewayConfig) => {
    setConfig((prev) => ({
      ...prev,
      ...res,
      sms_provider: res.sms_provider ?? '',
      war_room_provider: res.war_room_provider ?? '',
      smtp_tls_mode: res.smtp_tls_mode ?? 'ssl',
      smtp_port: res.smtp_port || 465,
      smtp_timeout_seconds: res.smtp_timeout_seconds || 10,
      aliyun_access_key_secret: '',
      tencent_secret_key: '',
      smtp_password: '',
      feishu_verification_token: '',
      feishu_encrypt_key: '',
      dingtalk_app_secret: '',
      feishu_app_secret: '',
      war_room_webhook_secret: '',
    }));
  };

  const fetchConfig = useCallback(async () => {
    setLoading(true);
    try {
      const res = await getChannelConfig();
      if (res) {
        applySanitizedResponse(res);
      }
    } catch (e) {
      console.error('Failed to load channel config:', e);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchConfig();
  }, [fetchConfig]);

  const handleSave = async () => {
    if (!isAdmin) return;
    setSaving(true);
    setSaveSuccess(false);
    try {
      const res = await updateChannelConfig({
        ...config,
        smtp_port: Math.min(65535, Math.max(1, Number(config.smtp_port) || 465)),
        smtp_timeout_seconds: Math.min(30, Math.max(3, Number(config.smtp_timeout_seconds) || 10)),
      });
      if (res?.config) {
        applySanitizedResponse(res.config);
      }
      setSaveSuccess(true);
      setTimeout(() => setSaveSuccess(false), 3000);
    } catch (e: unknown) {
      await alertAction(e instanceof Error ? e.message : tr('保存通道网关配置失败', 'Failed to save channel config'));
    } finally {
      setSaving(false);
    }
  };

  const handleTestSMS = async () => {
    if (!isAdmin) return;
    setTestingSMS(true);
    setSmsResult(null);
    try {
      const res = await testSMSChannel({
        phone_number: testPhone.trim(),
        urgency: 'high',
        title: tr('OpsPilot 值班告警短信连通性测试', 'OpsPilot SMS connectivity test'),
      });
      setSmsResult(res);
    } catch (e: unknown) {
      setSmsResult({
        provider: config.sms_provider || 'unconfigured',
        status: 'failed',
        phone_number: testPhone.trim(),
        message: e instanceof Error ? e.message : tr('短信测试请求失败', 'SMS test request failed'),
      });
    } finally {
      setTestingSMS(false);
    }
  };

  const handleTestEmail = async () => {
    if (!isAdmin) return;
    setTestingEmail(true);
    setEmailResult(null);
    try {
      const res = await testEmailChannel({
        to: testEmail.trim(),
        urgency: 'high',
        subject: tr('[OpsPilot On-Call] 邮件网关连通性测试', '[OpsPilot On-Call] Email Gateway Connectivity Test'),
      });
      setEmailResult(res);
    } catch (e: unknown) {
      setEmailResult({
        provider: 'smtp',
        status: 'failed',
        to: testEmail.trim(),
        subject: '',
        message: e instanceof Error ? e.message : tr('邮件测试请求失败', 'Email test request failed'),
      });
    } finally {
      setTestingEmail(false);
    }
  };

  const handleCopyURL = async (key: string, value: string) => {
    try {
      await navigator.clipboard.writeText(value);
      setCopiedKey(key);
      setTimeout(() => setCopiedKey(null), 2000);
    } catch {
      await alertAction(value);
    }
  };

  return (
    <div className="space-y-6 pt-4 border-t border-border">
      {/* Header Bar */}
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="min-w-0 flex-1">
          <h2 className="text-base font-semibold text-text">
            {tr('多通道通知与协同网关', 'Multi-Channel & Collaboration Gateways')}
          </h2>
          <p className="text-xs text-text-muted mt-0.5">
            {tr(
              '配置短信网关、邮件服务器以及飞书和钉钉卡片回调与应急作战室自动建群。',
              'Configure SMS providers, SMTP email delivery, Feishu/DingTalk interactive card callbacks, and War Room provisioning.'
            )}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-2 ml-auto">
          <Button
            size="sm"
            variant="outline"
            onClick={fetchConfig}
            disabled={loading}
            aria-label={tr('刷新通道网关配置', 'Refresh channel config')}
            title={tr('刷新通道网关配置', 'Refresh channel config')}
          >
            <RefreshCw size={14} className={loading ? 'animate-spin' : ''} />
          </Button>
          {isAdmin && (
            <Button size="sm" variant="primary" onClick={handleSave} disabled={saving}>
              <Save size={14} className="mr-1.5" />
              {saving ? tr('保存中...', 'Saving...') : tr('保存多通道配置', 'Save Channel Config')}
            </Button>
          )}
        </div>
      </div>

      {!isAdmin && (
        <div className="flex items-center gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3.5 py-2.5 text-xs text-amber-700 dark:text-amber-300">
          <ShieldAlert size={15} className="shrink-0" />
          <span>
            {tr(
              '当前账号为只读权限，修改短信、邮件或作战室密钥与实网测试需要管理员权限。',
              'Read-only access. Administrator role is required to modify channel secrets or trigger live tests.'
            )}
          </span>
        </div>
      )}

      {saveSuccess && (
        <div className="rounded-lg border border-emerald-500/30 bg-emerald-500/10 p-3 text-xs text-emerald-700 dark:text-emerald-300 font-medium">
          {tr('多通道通知与协同网关配置已加密保存。', 'Channel gateway configuration saved encrypted at rest.')}
        </div>
      )}

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 items-start">
        {/* 1. SMS Gateway Card */}
        <Card className="p-5 space-y-4">
          <div className="flex items-center justify-between gap-2 border-b border-border pb-3">
            <div className="flex items-center gap-2">
              <MessageSquare size={16} className="text-indigo-500" />
              <h3 className="text-xs font-semibold text-text uppercase tracking-wider">
                {tr('短信通知网关', 'SMS Notification Gateway')}
              </h3>
            </div>
            <Switch
              checked={config.sms_enabled}
              onCheckedChange={(checked) => setConfig({ ...config, sms_enabled: checked })}
              disabled={!isAdmin}
              aria-label={tr('启用短信通知网关', 'Enable SMS gateway')}
            />
          </div>

          <div className="space-y-3 text-xs">
            <div>
              <Label className="text-xs">{tr('短信服务商', 'SMS Provider')}</Label>
              <Select
                value={config.sms_provider || 'none'}
                onValueChange={(val) =>
                  setConfig({ ...config, sms_provider: val === 'none' ? '' : (val as 'aliyun' | 'tencent') })
                }
                disabled={!isAdmin}
                options={[
                  { value: 'none', label: tr('未启用', 'Not Configured') },
                  { value: 'aliyun', label: tr('阿里云短信服务', 'Aliyun SMS') },
                  { value: 'tencent', label: tr('腾讯云短信服务', 'Tencent Cloud SMS') },
                ]}
                className="mt-1 w-full text-xs"
              />
            </div>

            {config.sms_provider === 'aliyun' && (
              <div className="space-y-3 pt-2 border-t border-border">
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  <div>
                    <Label className="text-xs">AccessKey ID</Label>
                    <Input
                      value={config.aliyun_access_key_id || ''}
                      onChange={(e) => setConfig({ ...config, aliyun_access_key_id: e.target.value })}
                      placeholder="LTAI5t..."
                      disabled={!isAdmin}
                      className="mt-1 text-xs font-mono"
                    />
                  </div>
                  <SecretField
                    label="AccessKey Secret"
                    value={config.aliyun_access_key_secret || ''}
                    configured={config.aliyun_secret_configured}
                    disabled={!isAdmin}
                    onChange={(v) => setConfig({ ...config, aliyun_access_key_secret: v })}
                  />
                </div>
                <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                  <div>
                    <Label className="text-xs">{tr('短信签名', 'Sign Name')}</Label>
                    <Input
                      value={config.aliyun_sign_name || ''}
                      onChange={(e) => setConfig({ ...config, aliyun_sign_name: e.target.value })}
                      placeholder="OpsPilot"
                      disabled={!isAdmin}
                      className="mt-1 text-xs"
                    />
                  </div>
                  <div>
                    <Label className="text-xs">{tr('短信模版 Code', 'Template Code')}</Label>
                    <Input
                      value={config.aliyun_template_code || ''}
                      onChange={(e) => setConfig({ ...config, aliyun_template_code: e.target.value })}
                      placeholder="SMS_12345678"
                      disabled={!isAdmin}
                      className="mt-1 text-xs font-mono"
                    />
                  </div>
                  <div>
                    <Label className="text-xs">{tr('服务地域', 'Region')}</Label>
                    <Input
                      value={config.aliyun_region || ''}
                      onChange={(e) => setConfig({ ...config, aliyun_region: e.target.value })}
                      placeholder="cn-hangzhou"
                      disabled={!isAdmin}
                      className="mt-1 text-xs font-mono"
                    />
                  </div>
                </div>
              </div>
            )}

            {config.sms_provider === 'tencent' && (
              <div className="space-y-3 pt-2 border-t border-border">
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  <div>
                    <Label className="text-xs">SecretId</Label>
                    <Input
                      value={config.tencent_secret_id || ''}
                      onChange={(e) => setConfig({ ...config, tencent_secret_id: e.target.value })}
                      placeholder="AKID..."
                      disabled={!isAdmin}
                      className="mt-1 text-xs font-mono"
                    />
                  </div>
                  <SecretField
                    label="SecretKey"
                    value={config.tencent_secret_key || ''}
                    configured={config.tencent_secret_configured}
                    disabled={!isAdmin}
                    onChange={(v) => setConfig({ ...config, tencent_secret_key: v })}
                  />
                </div>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  <div>
                    <Label className="text-xs">SmsSdkAppId</Label>
                    <Input
                      value={config.tencent_sdk_app_id || ''}
                      onChange={(e) => setConfig({ ...config, tencent_sdk_app_id: e.target.value })}
                      placeholder="1400000000"
                      disabled={!isAdmin}
                      className="mt-1 text-xs font-mono"
                    />
                  </div>
                  <div>
                    <Label className="text-xs">{tr('短信签名', 'Sign Name')}</Label>
                    <Input
                      value={config.tencent_sign_name || ''}
                      onChange={(e) => setConfig({ ...config, tencent_sign_name: e.target.value })}
                      placeholder="OpsPilot"
                      disabled={!isAdmin}
                      className="mt-1 text-xs"
                    />
                  </div>
                </div>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  <div>
                    <Label className="text-xs">{tr('模板 ID', 'Template ID')}</Label>
                    <Input
                      value={config.tencent_template_id || ''}
                      onChange={(e) => setConfig({ ...config, tencent_template_id: e.target.value })}
                      placeholder="123456"
                      disabled={!isAdmin}
                      className="mt-1 text-xs font-mono"
                    />
                  </div>
                  <div>
                    <Label className="text-xs">{tr('服务地域', 'Region')}</Label>
                    <Input
                      value={config.tencent_region || ''}
                      onChange={(e) => setConfig({ ...config, tencent_region: e.target.value })}
                      placeholder="ap-guangzhou"
                      disabled={!isAdmin}
                      className="mt-1 text-xs font-mono"
                    />
                  </div>
                </div>
              </div>
            )}

            <div className="pt-3 border-t border-border space-y-2">
              <Label className="text-xs">{tr('短信测试手机号', 'Test SMS Phone Number')}</Label>
              <div className="flex flex-wrap items-center gap-2">
                <Input
                  value={testPhone}
                  onChange={(e) => setTestPhone(e.target.value)}
                  placeholder="13800000001"
                  disabled={!isAdmin}
                  className="flex-1 min-w-[160px] text-xs font-mono"
                />
                <Button size="sm" variant="outline" onClick={handleTestSMS} disabled={!isAdmin || testingSMS}>
                  <Send size={13} className="mr-1.5" />
                  {testingSMS ? tr('发送中...', 'Sending...') : tr('发送测试短信', 'Send Test SMS')}
                </Button>
              </div>
              {smsResult && (
                <div
                  className={`rounded-md border p-2.5 text-xs ${
                    smsResult.status === 'sent'
                      ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
                      : 'border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-300'
                  }`}
                >
                  {smsResult.message}
                </div>
              )}
            </div>
          </div>
        </Card>

        {/* 2. Email SMTP Gateway Card */}
        <Card className="p-5 space-y-4">
          <div className="flex items-center justify-between gap-2 border-b border-border pb-3">
            <div className="flex items-center gap-2">
              <Mail size={16} className="text-indigo-500" />
              <h3 className="text-xs font-semibold text-text uppercase tracking-wider">
                {tr('邮件通知网关', 'SMTP Email Gateway')}
              </h3>
            </div>
            <Switch
              checked={config.email_enabled}
              onCheckedChange={(checked) => setConfig({ ...config, email_enabled: checked })}
              disabled={!isAdmin}
              aria-label={tr('启用邮件通知网关', 'Enable SMTP email gateway')}
            />
          </div>

          <div className="space-y-3 text-xs">
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
              <div className="sm:col-span-2">
                <Label className="text-xs">{tr('SMTP 服务器地址', 'SMTP Host')}</Label>
                <Input
                  value={config.smtp_host || ''}
                  onChange={(e) => setConfig({ ...config, smtp_host: e.target.value })}
                  placeholder="smtp.exmail.qq.com"
                  disabled={!isAdmin}
                  className="mt-1 text-xs font-mono"
                />
              </div>
              <div>
                <Label className="text-xs">{tr('端口', 'Port')}</Label>
                <Input
                  type="number"
                  min={1}
                  max={65535}
                  value={config.smtp_port}
                  onChange={(e) => setConfig({ ...config, smtp_port: Number(e.target.value) || 465 })}
                  disabled={!isAdmin}
                  className="mt-1 text-xs font-mono"
                />
              </div>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div>
                <Label className="text-xs">{tr('传输加密模式', 'TLS Mode')}</Label>
                <Select
                  value={config.smtp_tls_mode}
                  onValueChange={(val) => {
                    const mode = val as 'ssl' | 'starttls' | 'none';
                    const defaultPort = mode === 'starttls' ? 587 : mode === 'none' ? 25 : 465;
                    const isStdPort = config.smtp_port === 465 || config.smtp_port === 587 || config.smtp_port === 25;
                    setConfig({
                      ...config,
                      smtp_tls_mode: mode,
                      smtp_port: isStdPort ? defaultPort : config.smtp_port,
                    });
                  }}
                  disabled={!isAdmin}
                  options={[
                    { value: 'ssl', label: tr('SSL/TLS 直连', 'SSL / TLS') },
                    { value: 'starttls', label: 'STARTTLS' },
                    { value: 'none', label: tr('无加密（内网中继）', 'Plain Relay') },
                  ]}
                  className="mt-1 w-full text-xs"
                />
              </div>
              <div>
                <Label className="text-xs">{tr('超时时间 (秒)', 'Timeout (s)')}</Label>
                <Input
                  type="number"
                  min={3}
                  max={30}
                  value={config.smtp_timeout_seconds}
                  onChange={(e) =>
                    setConfig({ ...config, smtp_timeout_seconds: Number(e.target.value) || 10 })
                  }
                  disabled={!isAdmin}
                  className="mt-1 text-xs font-mono"
                />
              </div>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div>
                <Label className="text-xs">{tr('发件人地址', 'From Address')}</Label>
                <Input
                  value={config.smtp_from || ''}
                  onChange={(e) => setConfig({ ...config, smtp_from: e.target.value })}
                  placeholder="oncall@example.com"
                  disabled={!isAdmin}
                  className="mt-1 text-xs font-mono"
                />
              </div>
              <div>
                <Label className="text-xs">{tr('认证用户名', 'Username')}</Label>
                <Input
                  value={config.smtp_username || ''}
                  onChange={(e) => setConfig({ ...config, smtp_username: e.target.value })}
                  placeholder="oncall@example.com"
                  disabled={!isAdmin}
                  className="mt-1 text-xs font-mono"
                />
              </div>
            </div>

            <SecretField
              label={tr('认证密码或授权码', 'SMTP Password / App Token')}
              value={config.smtp_password || ''}
              configured={config.smtp_password_configured}
              disabled={!isAdmin}
              onChange={(v) => setConfig({ ...config, smtp_password: v })}
            />

            <div className="pt-3 border-t border-border space-y-2">
              <Label className="text-xs">{tr('测试收件邮箱', 'Test Recipient Email')}</Label>
              <div className="flex flex-wrap items-center gap-2">
                <Input
                  value={testEmail}
                  onChange={(e) => setTestEmail(e.target.value)}
                  placeholder={tr('留空则发送至当前管理员邮箱', 'Blank defaults to admin email')}
                  disabled={!isAdmin}
                  className="flex-1 min-w-[160px] text-xs font-mono"
                />
                <Button size="sm" variant="outline" onClick={handleTestEmail} disabled={!isAdmin || testingEmail}>
                  <Send size={13} className="mr-1.5" />
                  {testingEmail ? tr('发送中...', 'Sending...') : tr('发送测试邮件', 'Send Test Email')}
                </Button>
              </div>
              {emailResult && (
                <div
                  className={`rounded-md border p-2.5 text-xs ${
                    emailResult.status === 'sent'
                      ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
                      : 'border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-300'
                  }`}
                >
                  {emailResult.message}
                </div>
              )}
            </div>
          </div>
        </Card>
      </div>

      {/* 3. Interactive Card Callbacks & War Room Card */}
      <Card className="p-5 space-y-4">
        <div className="flex items-center gap-2 border-b border-border pb-3">
          <Shield size={16} className="text-indigo-500" />
          <h3 className="text-xs font-semibold text-text uppercase tracking-wider">
            {tr('飞书和钉钉卡片回调与应急作战室', 'Interactive Card Callbacks & War Room')}
          </h3>
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 text-xs">
          <div className="space-y-3">
            <SecretField
              label={tr('飞书 Verification Token', 'Feishu Verification Token')}
              value={config.feishu_verification_token || ''}
              configured={config.feishu_verification_configured}
              disabled={!isAdmin}
              onChange={(v) => setConfig({ ...config, feishu_verification_token: v })}
            />
            <SecretField
              label={tr('飞书 Encrypt Key', 'Feishu Encrypt Key')}
              value={config.feishu_encrypt_key || ''}
              configured={config.feishu_encrypt_configured}
              disabled={!isAdmin}
              onChange={(v) => setConfig({ ...config, feishu_encrypt_key: v })}
            />
            <SecretField
              label={tr('钉钉应用 AppSecret', 'DingTalk AppSecret')}
              value={config.dingtalk_app_secret || ''}
              configured={config.dingtalk_secret_configured}
              disabled={!isAdmin}
              onChange={(v) => setConfig({ ...config, dingtalk_app_secret: v })}
            />

            {isAdmin && config.callback_urls && Object.keys(config.callback_urls).length > 0 && (
              <div className="space-y-2 pt-2 border-t border-border">
                {config.callback_urls.feishu && (
                  <div className="space-y-1">
                    <Label className="text-xs text-text-muted">{tr('飞书卡片回调地址', 'Feishu Card Callback URL')}</Label>
                    <div className="flex items-center gap-2">
                      <Input readOnly value={config.callback_urls.feishu} className="text-xs font-mono flex-1 min-w-0" />
                      <Button
                        size="sm"
                        variant="outline"
                        type="button"
                        onClick={() => handleCopyURL('feishu', config.callback_urls!.feishu)}
                        aria-label={tr('复制飞书回调地址', 'Copy Feishu callback URL')}
                      >
                        {copiedKey === 'feishu' ? <Check size={13} /> : <Copy size={13} />}
                      </Button>
                    </div>
                  </div>
                )}
                {config.callback_urls.dingtalk && (
                  <div className="space-y-1">
                    <Label className="text-xs text-text-muted">{tr('钉钉卡片回调地址', 'DingTalk Card Callback URL')}</Label>
                    <div className="flex items-center gap-2">
                      <Input readOnly value={config.callback_urls.dingtalk} className="text-xs font-mono flex-1 min-w-0" />
                      <Button
                        size="sm"
                        variant="outline"
                        type="button"
                        onClick={() => handleCopyURL('dingtalk', config.callback_urls!.dingtalk)}
                        aria-label={tr('复制钉钉回调地址', 'Copy DingTalk callback URL')}
                      >
                        {copiedKey === 'dingtalk' ? <Check size={13} /> : <Copy size={13} />}
                      </Button>
                    </div>
                  </div>
                )}
              </div>
            )}
          </div>

          <div className="space-y-3">
            <div>
              <Label className="text-xs">{tr('应急作战室自动建群方式', 'War Room Provisioning Mode')}</Label>
              <Select
                value={config.war_room_provider || 'console'}
                onValueChange={(val) =>
                  setConfig({
                    ...config,
                    war_room_provider: val === 'console' ? '' : (val as 'feishu' | 'webhook'),
                  })
                }
                disabled={!isAdmin}
                options={[
                  { value: 'console', label: tr('仅控制台应急协同视图（默认）', 'OpsPilot Console Link (Default)') },
                  { value: 'feishu', label: tr('飞书开放平台自动建群', 'Feishu OpenAPI Auto-Group') },
                  { value: 'webhook', label: tr('自定义 Webhook 建群接口', 'Custom Webhook Endpoint') },
                ]}
                className="mt-1 w-full text-xs"
              />
            </div>

            {config.war_room_provider === 'feishu' && (
              <div className="space-y-3 pt-2 border-t border-border">
                <div>
                  <Label className="text-xs">{tr('飞书自建应用 App ID', 'Feishu App ID')}</Label>
                  <Input
                    value={config.feishu_app_id || ''}
                    onChange={(e) => setConfig({ ...config, feishu_app_id: e.target.value })}
                    placeholder="cli_..."
                    disabled={!isAdmin}
                    className="mt-1 text-xs font-mono"
                  />
                </div>
                <SecretField
                  label={tr('飞书自建应用 App Secret', 'Feishu App Secret')}
                  value={config.feishu_app_secret || ''}
                  configured={config.feishu_app_secret_configured}
                  disabled={!isAdmin}
                  onChange={(v) => setConfig({ ...config, feishu_app_secret: v })}
                />
              </div>
            )}

            {config.war_room_provider === 'webhook' && (
              <div className="space-y-3 pt-2 border-t border-border">
                <div>
                  <Label className="text-xs">{tr('作战室 Webhook URL', 'War Room Webhook URL')}</Label>
                  <Input
                    value={config.war_room_webhook_url || ''}
                    onChange={(e) => setConfig({ ...config, war_room_webhook_url: e.target.value })}
                    placeholder="https://ops-bot.example.com/api/warroom"
                    disabled={!isAdmin}
                    className="mt-1 text-xs font-mono"
                  />
                </div>
                <SecretField
                  label={tr('Webhook 签名密钥', 'Webhook Signing Secret')}
                  value={config.war_room_webhook_secret || ''}
                  configured={config.war_room_webhook_secret_configured}
                  disabled={!isAdmin}
                  onChange={(v) => setConfig({ ...config, war_room_webhook_secret: v })}
                />
              </div>
            )}

            <p className="text-xs text-text-muted">
              {tr(
                '当触发应急作战室动作时，系统将按此配置创建或复用协同群并将链接广播至已启用的群聊机器人。',
                'Creates or reuses an incident collaboration chat room and broadcasts its entry link to active IM channels.'
              )}
            </p>
          </div>
        </div>
      </Card>

      {dialog}
    </div>
  );
}
