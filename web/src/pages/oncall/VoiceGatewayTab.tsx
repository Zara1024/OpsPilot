import { useState, useEffect, useCallback } from 'react';
import {
  PhoneCall,
  Settings,
  Save,
  RefreshCw,
  Copy,
  Check,
  ShieldAlert,
  RotateCw,
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
  getVoiceConfig,
  updateVoiceConfig,
  testVoiceCall,
  rotateVoiceCallbackToken,
  type VoiceGatewayConfig,
  type VoiceCallResult,
} from '@/api/oncall';

export function VoiceGatewayTab() {
  const { tr } = useI18n();
  const { isAdmin } = usePermissions();
  const { alertAction, confirmAction, dialog } = useDialogs();

  const [config, setConfig] = useState<VoiceGatewayConfig>({
    provider: 'mock',
    fallback_provider: '',
    failover_timeout_seconds: 8,
    dispatch_enabled: true,
    voice_redial_max_attempts: 1,
    voice_redial_delay_seconds: 20,
    voice_fast_escalate_on_reject: true,
    voice_redial_on_no_dtmf: false,
    aliyun_access_key_id: '',
    aliyun_access_key_secret: '',
    aliyun_secret_configured: false,
    aliyun_called_show_number: '',
    aliyun_tts_code: 'TTS_12345678',
    aliyun_region: 'cn-hangzhou',
    tencent_secret_id: '',
    tencent_secret_key: '',
    tencent_secret_configured: false,
    tencent_sdk_app_id: '',
    tencent_template_id: '',
    tencent_called_show_number: '',
    tencent_region: 'ap-guangzhou',
    callback_urls: {},
  });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [rotatingToken, setRotatingToken] = useState(false);
  const [rotateSuccess, setRotateSuccess] = useState(false);
  const [saveSuccess, setSaveSuccess] = useState(false);
  const [copiedKey, setCopiedKey] = useState<string | null>(null);

  // Test call state
  const [testPhone, setTestPhone] = useState('13800000001');
  const [testTitle, setTestTitle] = useState(() =>
    tr('生产核心订单支付接口超时率突增（>5%）', 'Order payment API timeout rate spike (>5%)'),
  );
  const [testUrgency, setTestUrgency] = useState<'high' | 'low'>('high');
  const [forceMock, setForceMock] = useState(false);
  const [calling, setCalling] = useState(false);
  const [callResult, setCallResult] = useState<VoiceCallResult | null>(null);

  const fetchConfig = useCallback(async () => {
    setLoading(true);
    try {
      const res = await getVoiceConfig();
      if (res) {
        setConfig((prev) => ({
          ...prev,
          ...res,
          fallback_provider: res.fallback_provider ?? '',
          failover_timeout_seconds: res.failover_timeout_seconds ?? 8,
          dispatch_enabled: res.dispatch_enabled ?? true,
          voice_redial_max_attempts: res.voice_redial_max_attempts ?? 1,
          voice_redial_delay_seconds: res.voice_redial_delay_seconds ?? 20,
          voice_fast_escalate_on_reject: res.voice_fast_escalate_on_reject ?? true,
          voice_redial_on_no_dtmf: res.voice_redial_on_no_dtmf ?? false,
          aliyun_access_key_secret: '',
          tencent_secret_key: '',
        }));
      }
    } catch (e) {
      console.error('Failed to load voice config:', e);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchConfig();
  }, [fetchConfig]);

  const handleSaveConfig = async () => {
    if (!isAdmin) return;
    setSaving(true);
    setSaveSuccess(false);
    try {
      const rawAttempts = config.voice_redial_max_attempts;
      const safeRedialMax =
        rawAttempts == null || !Number.isFinite(Number(rawAttempts))
          ? 1
          : Math.min(3, Math.max(0, Math.floor(Number(rawAttempts))));
      const rawDelay = config.voice_redial_delay_seconds;
      const safeRedialDelay =
        rawDelay == null || !Number.isFinite(Number(rawDelay)) || Number(rawDelay) <= 0
          ? 20
          : Math.min(120, Math.max(10, Math.floor(Number(rawDelay))));

      const payload: VoiceGatewayConfig = {
        ...config,
        fallback_provider: config.provider === 'mock' ? '' : (config.fallback_provider || ''),
        failover_timeout_seconds: Math.min(30, Math.max(3, Number(config.failover_timeout_seconds) || 8)),
        voice_redial_max_attempts: safeRedialMax,
        voice_redial_delay_seconds: safeRedialDelay,
        voice_fast_escalate_on_reject: config.voice_fast_escalate_on_reject ?? true,
        voice_redial_on_no_dtmf: config.voice_redial_on_no_dtmf ?? false,
      };
      const res = await updateVoiceConfig(payload);
      if (res?.config) {
        setConfig((prev) => ({
          ...prev,
          ...res.config,
          fallback_provider: res.config.fallback_provider ?? '',
          failover_timeout_seconds: res.config.failover_timeout_seconds ?? 8,
          voice_redial_max_attempts: res.config.voice_redial_max_attempts ?? safeRedialMax,
          voice_redial_delay_seconds: res.config.voice_redial_delay_seconds ?? safeRedialDelay,
          voice_fast_escalate_on_reject: res.config.voice_fast_escalate_on_reject ?? true,
          voice_redial_on_no_dtmf: res.config.voice_redial_on_no_dtmf ?? false,
          aliyun_access_key_secret: '',
          tencent_secret_key: '',
        }));
      }
      setSaveSuccess(true);
      setTimeout(() => setSaveSuccess(false), 3000);
    } catch (e: unknown) {
      console.error('Failed to update voice config:', e);
      await alertAction(e instanceof Error ? e.message : tr('保存失败', 'Failed to save'));
    } finally {
      setSaving(false);
    }
  };

  const renderAliyunFields = (sectionTitle: string) => (
    <div className="space-y-3 pt-2 border-t border-border">
      <span className="font-semibold text-text block">{sectionTitle}</span>
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
        <div>
          <div className="flex items-center justify-between gap-2">
            <Label className="text-xs">AccessKey Secret</Label>
            {config.aliyun_secret_configured && (
              <Chip tone="success" dense>{tr('已加密存储', 'Encrypted')}</Chip>
            )}
          </div>
          <Input
            type="password"
            value={config.aliyun_access_key_secret || ''}
            onChange={(e) => setConfig({ ...config, aliyun_access_key_secret: e.target.value })}
            placeholder={
              config.aliyun_secret_configured
                ? tr('已配置密钥，留空保持不变', 'Configured, leave blank to keep')
                : '••••••••••••'
            }
            disabled={!isAdmin}
            className="mt-1 text-xs font-mono"
          />
        </div>
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
        <div>
          <Label className="text-xs">{tr('主叫显号', 'Called Show Number')}</Label>
          <Input
            value={config.aliyun_called_show_number || ''}
            onChange={(e) => setConfig({ ...config, aliyun_called_show_number: e.target.value })}
            placeholder="057188888888"
            disabled={!isAdmin}
            className="mt-1 text-xs font-mono"
          />
        </div>
        <div>
          <Label className="text-xs">{tr('TTS 语音模版 Code', 'TTS Template Code')}</Label>
          <Input
            value={config.aliyun_tts_code || ''}
            onChange={(e) => setConfig({ ...config, aliyun_tts_code: e.target.value })}
            placeholder="TTS_12345678"
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
      <p className="text-xs text-text-muted">
        {tr(
          '阿里云 TTS 模板变量默认传入 JSON 字段 urgency 与 title，回调支持按 1 认领、按 2 升级。',
          'Aliyun TTS parameters default to urgency and title; DTMF 1 acknowledges and DTMF 2 escalates.'
        )}
      </p>
    </div>
  );

  const renderTencentFields = (sectionTitle: string) => (
    <div className="space-y-3 pt-2 border-t border-border">
      <span className="font-semibold text-text block">{sectionTitle}</span>
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
        <div>
          <div className="flex items-center justify-between gap-2">
            <Label className="text-xs">SecretKey</Label>
            {config.tencent_secret_configured && (
              <Chip tone="success" dense>{tr('已加密存储', 'Encrypted')}</Chip>
            )}
          </div>
          <Input
            type="password"
            value={config.tencent_secret_key || ''}
            onChange={(e) => setConfig({ ...config, tencent_secret_key: e.target.value })}
            placeholder={
              config.tencent_secret_configured
                ? tr('已配置密钥，留空保持不变', 'Configured, leave blank to keep')
                : '••••••••••••'
            }
            disabled={!isAdmin}
            className="mt-1 text-xs font-mono"
          />
        </div>
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <div>
          <Label className="text-xs">SdkAppId</Label>
          <Input
            value={config.tencent_sdk_app_id || ''}
            onChange={(e) => setConfig({ ...config, tencent_sdk_app_id: e.target.value })}
            placeholder="1400000000"
            disabled={!isAdmin}
            className="mt-1 text-xs font-mono"
          />
        </div>
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
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <div>
          <Label className="text-xs">{tr('主叫显号', 'Called Show Number')}</Label>
          <Input
            value={config.tencent_called_show_number || ''}
            onChange={(e) => setConfig({ ...config, tencent_called_show_number: e.target.value })}
            placeholder="+861088888888"
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
      <p className="text-xs text-text-muted">
        {tr(
          '腾讯云模板参数按顺序传入 {1}=紧急度、{2}=告警标题，回调支持按 1 认领、按 2 升级。',
          'Tencent template params map {1}=urgency and {2}=title; DTMF 1 acknowledges and DTMF 2 escalates.'
        )}
      </p>
    </div>
  );

  const handleTestCall = async () => {
    if (!isAdmin) return;
    if (!testPhone.trim()) {
      await alertAction(tr('请输入测试手机号码', 'Please enter a phone number'));
      return;
    }
    setCalling(true);
    setCallResult(null);
    try {
      const res = await testVoiceCall({
        phone_number: testPhone.trim(),
        title: testTitle.trim(),
        urgency: testUrgency,
        severity: testUrgency === 'high' ? 'P1' : 'P3',
        force_mock: config.provider !== 'mock' && forceMock,
      });
      setCallResult(res);
    } catch (e: unknown) {
      console.error('Failed to trigger test voice call:', e);
      await alertAction(e instanceof Error ? e.message : tr('外呼测试失败', 'Call failed'));
    } finally {
      setCalling(false);
    }
  };

  const handleCopyURL = async (key: string, url: string) => {
    try {
      await navigator.clipboard?.writeText(url);
      setCopiedKey(key);
      setTimeout(() => setCopiedKey(null), 2000);
    } catch {
      await alertAction(tr('复制失败，请手动选中复制', 'Copy failed, please copy manually'));
    }
  };

  const handleRotateToken = async () => {
    if (!isAdmin || rotatingToken) return;
    const confirmed = await confirmAction(
      tr(
        '确认轮换语音回调 Token 吗？生成新地址后，原回调 URL 将立即失效，请同步更新云厂商后台配置。',
        'Rotate the voice webhook token? Existing callback URLs will stop working immediately.'
      ),
    );
    if (!confirmed) return;
    setRotatingToken(true);
    setRotateSuccess(false);
    try {
      const res = await rotateVoiceCallbackToken();
      if (res?.config) {
        setConfig((prev) => ({ ...prev, ...res.config }));
      }
      setRotateSuccess(true);
      setTimeout(() => setRotateSuccess(false), 5000);
    } catch (e: unknown) {
      await alertAction(e instanceof Error ? e.message : tr('轮换回调 Token 失败', 'Failed to rotate callback token'));
    } finally {
      setRotatingToken(false);
    }
  };

  const renderCallStatusChip = (status: VoiceCallResult['status']) => {
    switch (status) {
      case 'failed':
        return <Chip tone="danger" dense>{tr('呼叫失败', 'Failed')}</Chip>;
      case 'rejected':
        return <Chip tone="danger" dense>{tr('被叫拒接', 'Rejected')}</Chip>;
      case 'unreachable':
        return <Chip tone="danger" dense>{tr('不可达/关机', 'Unreachable')}</Chip>;
      case 'no_answer':
        return <Chip tone="warning" dense>{tr('无人接听', 'No Answer')}</Chip>;
      case 'calling':
        return <Chip tone="accent" dense>{tr('已发起呼叫', 'Calling')}</Chip>;
      case 'throttled':
        return <Chip tone="warning" dense>{tr('频控拦截', 'Throttled')}</Chip>;
      case 'dtmf_confirmed':
      case 'answered':
        return <Chip tone="success" dense>{tr('已接通确认', 'Confirmed')}</Chip>;
      default:
        return <Chip tone="default" dense>{status}</Chip>;
    }
  };

  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="min-w-0">
          <h2 className="text-base font-semibold text-text">
            {tr('多云语音网关基础设施', 'Multi-Cloud Voice Gateway')}
          </h2>
          <p className="text-xs text-text-muted mt-0.5">
            {tr(
              '夜间生命线强触达保障，对接阿里云语音服务、腾讯云外呼及本地仿真网关，支持 DTMF 按键接单确认。',
              'Voice call lifeline delivery via Aliyun, Tencent Cloud, or Mock Gateway with DTMF confirmation.'
            )}
          </p>
        </div>

        <Button
          size="sm"
          variant="ghost"
          onClick={fetchConfig}
          disabled={loading}
          aria-label={tr('刷新语音网关配置', 'Refresh voice gateway configuration')}
          title={tr('刷新语音网关配置', 'Refresh voice gateway configuration')}
        >
          <RefreshCw size={14} className={loading ? 'animate-spin' : ''} />
        </Button>
      </div>

      {!isAdmin && (
        <div
          role="status"
          className="flex items-center gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3.5 py-2.5 text-xs text-amber-700 dark:text-amber-300"
        >
          <ShieldAlert size={15} className="shrink-0" />
          <span>
            {tr(
              '当前账号为只读权限，修改云厂商密钥配置及发起实网外呼需要管理员权限。',
              'Read-only access. Administrator role is required to edit provider credentials or trigger live voice calls.'
            )}
          </span>
        </div>
      )}

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Gateway Configuration Card */}
        <Card className="p-5 space-y-4">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border pb-3">
            <div className="flex items-center gap-2">
              <Settings size={16} className="text-indigo-500" />
              <h3 className="text-xs font-semibold text-text uppercase tracking-wider">
                {tr('语音服务商配置', 'Provider Config')}
              </h3>
            </div>
            <Button
              size="sm"
              variant="primary"
              onClick={handleSaveConfig}
              disabled={saving || !isAdmin}
            >
              <Save size={14} className="mr-1.5" />
              {saving ? tr('保存中...', 'Saving...') : tr('保存网关配置', 'Save Config')}
            </Button>
          </div>

          {saveSuccess && (
            <div className="rounded bg-emerald-500/10 border border-emerald-500/30 p-2 text-xs text-emerald-600 dark:text-emerald-400">
              {tr('语音网关配置已加密保存。', 'Voice gateway configuration encrypted and saved.')}
            </div>
          )}

          <div className="space-y-4 text-xs">
            <div className="flex items-center justify-between gap-3 rounded-lg border border-border bg-bg p-3">
              <div className="min-w-0">
                <div className="font-medium text-text">
                  {tr('启用告警自动值班派发与语音升级', 'Enable automated alert on-call dispatch')}
                </div>
                <div className="text-text-muted mt-0.5">
                  {tr(
                    '关闭后，新触发的告警将暂停自动进入值班升级与语音外呼队列。',
                    'When disabled, newly fired alerts will not be queued into automated escalation.'
                  )}
                </div>
              </div>
              <Switch
                checked={Boolean(config.dispatch_enabled)}
                onCheckedChange={(checked) => setConfig({ ...config, dispatch_enabled: checked })}
                disabled={!isAdmin}
                aria-label={tr('启用告警自动值班派发与语音升级', 'Enable automated alert on-call dispatch')}
              />
            </div>

            <div>
              <Label className="text-xs">{tr('选择默认语音服务商', 'Active Provider')}</Label>
              <Select
                value={config.provider}
                onValueChange={(val) => {
                  const nextProvider = val as VoiceGatewayConfig['provider'];
                  const nextFallback =
                    nextProvider === 'mock' || nextProvider === config.fallback_provider
                      ? ''
                      : config.fallback_provider || '';
                  setConfig({ ...config, provider: nextProvider, fallback_provider: nextFallback });
                }}
                disabled={!isAdmin}
                options={[
                  { value: 'mock', label: tr('本地仿真网关', 'Mock Gateway') },
                  { value: 'aliyun', label: tr('阿里云智能语音服务', 'Aliyun Voice') },
                  { value: 'tencent', label: tr('腾讯云语音消息外呼', 'Tencent Voice') },
                ]}
                className="mt-1 w-full text-xs"
              />
            </div>

            {config.provider !== 'mock' && (
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 rounded-lg border border-border bg-bg/40 p-3">
                <div className="sm:col-span-2">
                  <Label className="text-xs">{tr('备援语音服务商', 'Backup Provider')}</Label>
                  <Select
                    value={config.fallback_provider || 'none'}
                    onValueChange={(val) =>
                      setConfig({
                        ...config,
                        fallback_provider: val === 'none' ? '' : (val as 'aliyun' | 'tencent'),
                      })
                    }
                    disabled={!isAdmin}
                    options={[
                      { value: 'none', label: tr('无备援（单通道）', 'None (Single Provider)') },
                      config.provider === 'aliyun'
                        ? { value: 'tencent', label: tr('腾讯云语音消息外呼', 'Tencent Voice') }
                        : { value: 'aliyun', label: tr('阿里云智能语音服务', 'Aliyun Voice') },
                    ]}
                    className="mt-1 w-full text-xs"
                  />
                </div>
                <div>
                  <Label className="text-xs">{tr('主通道超时阈值 (秒)', 'Failover Timeout (s)')}</Label>
                  <Input
                    type="number"
                    min={3}
                    max={30}
                    value={config.failover_timeout_seconds ?? 8}
                    onChange={(e) => {
                      const v = Number(e.target.value);
                      setConfig({
                        ...config,
                        failover_timeout_seconds: Number.isFinite(v) ? v : 8,
                      });
                    }}
                    disabled={!isAdmin}
                    className="mt-1 text-xs font-mono"
                  />
                </div>
              </div>
            )}

            {config.provider === 'mock' && (
              <div className="rounded-lg border border-indigo-500/30 bg-indigo-50/20 dark:bg-indigo-950/20 p-3 text-xs text-indigo-700 dark:text-indigo-300">
                {tr(
                  '当前处于本地仿真网关模式。测试时将模拟完整外呼状态流转与 DTMF 确认，不消耗云厂商语音额度。',
                  'Mock Gateway is active. Simulates call state transitions and DTMF confirmation without telecom charges.'
                )}
              </div>
            )}

            {config.provider === 'aliyun' && renderAliyunFields(tr('阿里云语音参数', 'Aliyun Settings'))}
            {config.provider === 'tencent' && renderTencentFields(tr('腾讯云语音参数', 'Tencent Settings'))}
            {config.provider === 'aliyun' &&
              config.fallback_provider === 'tencent' &&
              renderTencentFields(tr('备援通道：腾讯云语音参数', 'Backup: Tencent Settings'))}
            {config.provider === 'tencent' &&
              config.fallback_provider === 'aliyun' &&
              renderAliyunFields(tr('备援通道：阿里云语音参数', 'Backup: Aliyun Settings'))}

            {/* Redial & Fast-Escalate Policy */}
            <div className="space-y-3 pt-3 border-t border-border">
              <div className="font-semibold text-text">
                {tr('语音回执重拨与快进升级策略', 'Voice Redial & Fast-Escalate Policy')}
              </div>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <div>
                  <Label className="text-xs">
                    {tr('无人接听最大重拨次数 (0-3)', 'Max Auto-Redials on No Answer (0–3)')}
                  </Label>
                  <Input
                    type="number"
                    min={0}
                    max={3}
                    value={config.voice_redial_max_attempts ?? 1}
                    onChange={(e) => {
                      const raw = e.target.value;
                      const v = Number(raw);
                      setConfig({
                        ...config,
                        voice_redial_max_attempts:
                          raw === '' || !Number.isFinite(v) ? 0 : Math.min(3, Math.max(0, Math.floor(v))),
                      });
                    }}
                    disabled={!isAdmin}
                    aria-label={tr('无人接听最大重拨次数', 'Max auto-redials on no answer')}
                    className="mt-1 text-xs font-mono"
                  />
                </div>
                <div>
                  <Label className="text-xs">
                    {tr('重拨冷却间隔 (10-120秒)', 'Redial Delay (10–120s)')}
                  </Label>
                  <Input
                    type="number"
                    min={10}
                    max={120}
                    value={config.voice_redial_delay_seconds ?? 20}
                    onChange={(e) => {
                      const v = Number(e.target.value);
                      setConfig({
                        ...config,
                        voice_redial_delay_seconds: Number.isFinite(v) ? v : 20,
                      });
                    }}
                    disabled={!isAdmin || (config.voice_redial_max_attempts ?? 1) === 0}
                    aria-label={tr('重拨冷却间隔', 'Redial delay seconds')}
                    className="mt-1 text-xs font-mono"
                  />
                </div>
              </div>

              <div className="space-y-2 rounded-lg border border-border bg-bg/40 p-3">
                <div className="flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <div className="font-medium text-text">
                      {tr('拒接 / 关机 / 空号立即快进升级', 'Fast-Escalate on Reject or Unreachable')}
                    </div>
                    <div className="text-[11px] text-text-muted mt-0.5">
                      {tr(
                        '收到被叫拒接、挂断或停机空号回执时跳过剩余等待时间，立即唤醒下一升级梯队。',
                        'Immediately advance to the next escalation step when carriers report rejected or unreachable calls.'
                      )}
                    </div>
                  </div>
                  <Switch
                    checked={config.voice_fast_escalate_on_reject ?? true}
                    onCheckedChange={(checked) =>
                      setConfig({ ...config, voice_fast_escalate_on_reject: checked })
                    }
                    disabled={!isAdmin}
                    aria-label={tr('拒接 / 关机 / 空号立即快进升级', 'Fast-Escalate on Reject or Unreachable')}
                  />
                </div>

                <div className="border-t border-border pt-2 flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <div className="font-medium text-text">
                      {tr('已接听但未按键时也触发自动重拨', 'Auto-Redial When Answered Without DTMF')}
                    </div>
                    <div className="text-[11px] text-text-muted mt-0.5">
                      {tr(
                        '默认关闭（仅等待正常升级计时）；开启后，若值班人接听后未按 1 或 2 即挂断也将按重拨策略补呼。',
                        'Off by default; when enabled, answered calls with no DTMF input also follow the automatic redial budget.'
                      )}
                    </div>
                  </div>
                  <Switch
                    checked={config.voice_redial_on_no_dtmf ?? false}
                    onCheckedChange={(checked) =>
                      setConfig({ ...config, voice_redial_on_no_dtmf: checked })
                    }
                    disabled={!isAdmin}
                    aria-label={tr('已接听但未按键时也触发自动重拨', 'Auto-Redial When Answered Without DTMF')}
                  />
                </div>
              </div>
            </div>

            {isAdmin && config.callback_urls && Object.keys(config.callback_urls).length > 0 && (
              <div className="space-y-2.5 pt-3 border-t border-border">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="font-semibold text-text">
                    {tr('语音状态与 DTMF 按键回执地址', 'Signed Status & DTMF Callback URLs')}
                  </div>
                  <Button
                    size="sm"
                    variant="dangerGhost"
                    type="button"
                    onClick={handleRotateToken}
                    disabled={rotatingToken}
                  >
                    <RotateCw size={12} className={`mr-1 ${rotatingToken ? 'animate-spin' : ''}`} />
                    {rotatingToken ? tr('轮换中...', 'Rotating...') : tr('一键轮换回调 Token', 'Rotate Callback Token')}
                  </Button>
                </div>
                {rotateSuccess && (
                  <div className="rounded-md border border-emerald-500/30 bg-emerald-500/10 p-2 text-xs text-emerald-700 dark:text-emerald-300">
                    {tr('语音回调 Token 已轮换生效，请将下方新地址同步至阿里云/腾讯云控制台。', 'Voice callback token rotated. Update your cloud provider webhook URLs.')}
                  </div>
                )}
                {config.callback_urls.aliyun && (
                  <div className="space-y-1">
                    <Label className="text-xs text-text-muted">
                      {tr('阿里云回调地址', 'Aliyun Callback URL')}
                    </Label>
                    <div className="flex items-center gap-2">
                      <Input
                        readOnly
                        value={config.callback_urls.aliyun}
                        className="text-xs font-mono flex-1 min-w-0"
                      />
                      <Button
                        size="sm"
                        variant="outline"
                        type="button"
                        onClick={() => handleCopyURL('aliyun', config.callback_urls!.aliyun)}
                        aria-label={tr('复制阿里云回调地址', 'Copy Aliyun callback URL')}
                      >
                        {copiedKey === 'aliyun' ? <Check size={13} /> : <Copy size={13} />}
                      </Button>
                    </div>
                  </div>
                )}
                {config.callback_urls.tencent && (
                  <div className="space-y-1">
                    <Label className="text-xs text-text-muted">
                      {tr('腾讯云回调地址', 'Tencent Callback URL')}
                    </Label>
                    <div className="flex items-center gap-2">
                      <Input
                        readOnly
                        value={config.callback_urls.tencent}
                        className="text-xs font-mono flex-1 min-w-0"
                      />
                      <Button
                        size="sm"
                        variant="outline"
                        type="button"
                        onClick={() => handleCopyURL('tencent', config.callback_urls!.tencent)}
                        aria-label={tr('复制腾讯云回调地址', 'Copy Tencent callback URL')}
                      >
                        {copiedKey === 'tencent' ? <Check size={13} /> : <Copy size={13} />}
                      </Button>
                    </div>
                  </div>
                )}
              </div>
            )}
          </div>
        </Card>

        {/* Test Voice Call Simulator */}
        <Card className="p-5 space-y-4">
          <div className="flex items-center gap-2 border-b border-border pb-3">
            <PhoneCall size={16} className="text-indigo-500" />
            <h3 className="text-xs font-semibold text-text uppercase tracking-wider">
              {tr('在线语音外呼实测', 'Voice Call Test')}
            </h3>
          </div>

          <p className="text-xs text-text-muted">
            {tr(
              '发起测试语音外呼以验证厂商签名、TTS 参数拼装及回执记录链路。',
              'Trigger a test voice call to verify provider signatures, TTS parameters, and callback tracking.'
            )}
          </p>

          <div className="space-y-3 text-xs">
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
              <div className="sm:col-span-2">
                <Label className="text-xs">{tr('测试目标手机号', 'Target Phone Number')} *</Label>
                <Input
                  value={testPhone}
                  onChange={(e) => setTestPhone(e.target.value)}
                  placeholder="13800000001"
                  disabled={!isAdmin}
                  className="mt-1 text-xs font-mono"
                />
              </div>
              <div>
                <Label className="text-xs">{tr('紧急程度', 'Urgency')}</Label>
                <Select
                  value={testUrgency}
                  onValueChange={(val) => setTestUrgency(val as 'high' | 'low')}
                  disabled={!isAdmin}
                  options={[
                    { value: 'high', label: tr('高优先级', 'High') },
                    { value: 'low', label: tr('普通', 'Low') },
                  ]}
                  className="mt-1 w-full text-xs"
                />
              </div>
            </div>

            <div>
              <Label className="text-xs">{tr('测试告警标题', 'Alert Title for TTS')}</Label>
              <Input
                value={testTitle}
                onChange={(e) => setTestTitle(e.target.value)}
                placeholder={tr('订单支付服务超时告警', 'Order payment service timeout alert')}
                disabled={!isAdmin}
                className="mt-1 text-xs"
              />
            </div>

            {config.provider !== 'mock' && (
              <div className="flex items-center justify-between gap-3 rounded-lg border border-border bg-bg p-2.5">
                <span className="text-text-muted">
                  {tr('仅使用本地仿真测试（不消耗云厂商额度）', 'Use local mock simulation (zero telecom cost)')}
                </span>
                <Switch
                  checked={forceMock}
                  onCheckedChange={setForceMock}
                  disabled={!isAdmin}
                  aria-label={tr('仅使用本地仿真测试', 'Use local mock simulation')}
                />
              </div>
            )}

            <Button
              size="sm"
              variant="primary"
              onClick={handleTestCall}
              disabled={calling || !isAdmin}
              className="w-full"
            >
              <PhoneCall size={14} className="mr-1.5" />
              {calling ? tr('外呼拨打中...', 'Calling...') : tr('发起在线语音外呼测试', 'Trigger Voice Call')}
            </Button>

            {callResult && (
              <div className="mt-4 rounded-lg border border-border bg-bg p-3.5 space-y-2 font-mono text-xs">
                {callResult.fallback_from && (
                  <div className="rounded-md border border-amber-500/30 bg-amber-500/10 p-2.5 text-xs font-sans text-amber-700 dark:text-amber-300 space-y-1">
                    <div className="font-semibold">
                      {tr(
                        `主通道（${callResult.fallback_from}）外呼异常，已自动切换至备援通道（${callResult.provider}）`,
                        `Primary (${callResult.fallback_from}) failed; failed over to backup (${callResult.provider})`
                      )}
                    </div>
                    {callResult.primary_error && (
                      <div className="text-[11px] break-words">
                        {tr('主通道失败原因：', 'Primary Error: ')}
                        {callResult.primary_error}
                      </div>
                    )}
                  </div>
                )}
                <div className="flex items-center justify-between gap-2">
                  <span className="text-text-muted">{tr('呼叫流水号', 'Call ID')}:</span>
                  <span className="text-text font-bold truncate">{callResult.call_id}</span>
                </div>
                {callResult.out_call_id && (
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-text-muted">{tr('厂商回执号', 'Provider Call ID')}:</span>
                    <span className="text-text truncate">{callResult.out_call_id}</span>
                  </div>
                )}
                <div className="flex items-center justify-between gap-2">
                  <span className="text-text-muted">{tr('服务提供商', 'Provider')}:</span>
                  <span className="text-indigo-600 dark:text-indigo-400 uppercase font-semibold">
                    {callResult.provider}
                  </span>
                </div>
                <div className="flex items-center justify-between gap-2">
                  <span className="text-text-muted">{tr('呼叫状态', 'Status')}:</span>
                  {renderCallStatusChip(callResult.status)}
                </div>
                {callResult.dtmf_code && (
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-text-muted">{tr('DTMF 按键结果', 'DTMF Input')}:</span>
                    <span className="text-emerald-600 dark:text-emerald-400 font-bold">
                      {tr(`按键 '${callResult.dtmf_code}'（已确认认领接单）`, `Key '${callResult.dtmf_code}' (Acknowledged)`)}
                    </span>
                  </div>
                )}
                {callResult.message && (
                  <div className="border-t border-border pt-2 text-xs text-text-muted font-sans break-words">
                    <span className="text-text font-medium">{tr('结果说明：', 'Result: ')}</span>
                    <span className={callResult.status === 'failed' ? 'text-red-500 dark:text-red-400' : ''}>
                      {callResult.message}
                    </span>
                  </div>
                )}
                {callResult.tts_content && (
                  <div className="border-t border-border pt-2 text-xs text-text-muted font-sans break-words">
                    <span className="block text-text font-medium mb-0.5">{tr('TTS 播报内容：', 'TTS Content: ')}</span>
                    <span className="italic text-text-muted">{callResult.tts_content}</span>
                  </div>
                )}
              </div>
            )}
          </div>
        </Card>
      </div>
      {dialog}
    </div>
  );
}
