import { useState, useEffect, useCallback } from 'react';
import {
  PhoneCall,
  Settings,
  Save,
  RefreshCw,
} from 'lucide-react';
import {
  Button,
  Card,
  Chip,
  Input,
  Label,
  Select,
} from '@/components/ui';
import { useDialogs } from '@/components/ui/useDialogs';
import { useI18n } from '@/i18n/locale';
import {
  getVoiceConfig,
  updateVoiceConfig,
  testVoiceCall,
  type VoiceGatewayConfig,
  type VoiceCallResult,
} from '@/api/oncall';

export function VoiceGatewayTab() {
  const { tr } = useI18n();
  const { alertAction, dialog } = useDialogs();

  const [config, setConfig] = useState<VoiceGatewayConfig>({
    provider: 'mock',
    aliyun_access_key_id: '',
    aliyun_access_key_secret: '',
    aliyun_called_show_number: '',
    aliyun_tts_code: 'TTS_12345678',
    aliyun_region: 'cn-hangzhou',
    tencent_secret_id: '',
    tencent_secret_key: '',
    tencent_sdk_app_id: '',
    tencent_template_id: '',
    tencent_called_show_number: '',
    tencent_region: 'ap-guangzhou',
  });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [saveSuccess, setSaveSuccess] = useState(false);

  // Test call state
  const [testPhone, setTestPhone] = useState('13800000001');
  const [testTitle, setTestTitle] = useState(() =>
    tr('生产核心订单支付接口超时率突增（>5%）', 'Order payment API timeout rate spike (>5%)'),
  );
  const testUrgency: 'high' | 'low' = 'high';
  const [calling, setCalling] = useState(false);
  const [callResult, setCallResult] = useState<VoiceCallResult | null>(null);

  const fetchConfig = useCallback(async () => {
    setLoading(true);
    try {
      const res = await getVoiceConfig();
      if (res) {
        setConfig((prev) => ({ ...prev, ...res }));
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
    setSaving(true);
    setSaveSuccess(false);
    try {
      await updateVoiceConfig(config);
      setSaveSuccess(true);
      setTimeout(() => setSaveSuccess(false), 3000);
    } catch (e: unknown) {
      console.error('Failed to update voice config:', e);
      await alertAction(e instanceof Error ? e.message : tr('保存失败', 'Failed to save'));
    } finally {
      setSaving(false);
    }
  };

  const handleTestCall = async () => {
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
        severity: 'P1',
      });
      setCallResult(res);
    } catch (e: unknown) {
      console.error('Failed to trigger test voice call:', e);
      await alertAction(e instanceof Error ? e.message : tr('外呼测试失败', 'Call failed'));
    } finally {
      setCalling(false);
    }
  };

  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <h2 className="text-base font-semibold text-text">
            {tr('多云语音网关基础设施', 'Multi-Cloud Voice Gateway')}
          </h2>
          <p className="text-xs text-text-muted mt-0.5">
            {tr(
              '夜间生命线强触达保障，对接阿里云语音服务、腾讯云外呼及本地仿真网关，支持 DTMF 按键接单确认。',
              'Voice call lifeline delivery via Aliyun, Tencent Cloud, or Mock Gateway with DTMF acking.'
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

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Gateway Configuration Card */}
        <Card className="p-5 space-y-4">
          <div className="flex items-center justify-between border-b border-border pb-3">
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
              disabled={saving}
            >
              <Save size={14} className="mr-1.5" />
              {saving ? tr('保存中...', 'Saving...') : tr('保存网关配置', 'Save Config')}
            </Button>
          </div>

          {saveSuccess && (
            <div className="rounded bg-emerald-500/10 border border-emerald-500/30 p-2 text-xs text-emerald-600 dark:text-emerald-400">
              {tr('语音网关配置保存成功！', 'Voice gateway configuration saved successfully!')}
            </div>
          )}

          <div className="space-y-4 text-xs">
            <div>
              <Label className="text-xs">{tr('选择默认语音服务商', 'Active Provider')}</Label>
              <Select
                value={config.provider}
                onValueChange={(val) => setConfig({ ...config, provider: val as VoiceGatewayConfig['provider'] })}
                options={[
                  { value: 'mock', label: tr('本地仿真网关（推荐测试）', 'Mock Gateway (Recommended for dev)') },
                  { value: 'aliyun', label: tr('阿里云智能语音服务', 'Aliyun Voice') },
                  { value: 'tencent', label: tr('腾讯云语音消息外呼', 'Tencent Voice') },
                ]}
                className="mt-1 w-full text-xs"
              />
            </div>

            {config.provider === 'mock' && (
              <div className="rounded-lg border border-indigo-500/30 bg-indigo-50/20 dark:bg-indigo-950/20 p-3 text-xs text-indigo-700 dark:text-indigo-300">
                {tr(
                  '当前处于本地仿真网关模式。发起测试后将完整模拟真实外呼状态机流转（振铃 ➔ 接听 ➔ TTS播报 ➔ DTMF 按 1 确认接单）。无需消耗云厂商语音短信额度。',
                  'Mock Gateway active. Simulates realistic call state transitions and DTMF acks without telecom costs.'
                )}
              </div>
            )}

            {config.provider === 'aliyun' && (
              <div className="space-y-3 pt-2 border-t border-border">
                <span className="font-semibold text-text block">
                  {tr('阿里云语音参数', 'Aliyun Settings')}
                </span>
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <Label className="text-xs">AccessKey ID</Label>
                    <Input
                      value={config.aliyun_access_key_id || ''}
                      onChange={(e) => setConfig({ ...config, aliyun_access_key_id: e.target.value })}
                      placeholder="LTAI5t..."
                      className="mt-1 text-xs font-mono"
                    />
                  </div>
                  <div>
                    <Label className="text-xs">AccessKey Secret</Label>
                    <Input
                      type="password"
                      value={config.aliyun_access_key_secret || ''}
                      onChange={(e) => setConfig({ ...config, aliyun_access_key_secret: e.target.value })}
                      placeholder="••••••••••••"
                      className="mt-1 text-xs font-mono"
                    />
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <Label className="text-xs">{tr('主叫显号', 'Called Show Number')}</Label>
                    <Input
                      value={config.aliyun_called_show_number || ''}
                      onChange={(e) => setConfig({ ...config, aliyun_called_show_number: e.target.value })}
                      placeholder="057188888888"
                      className="mt-1 text-xs font-mono"
                    />
                  </div>
                  <div>
                    <Label className="text-xs">{tr('TTS 语音模版 Code', 'TTS Template Code')}</Label>
                    <Input
                      value={config.aliyun_tts_code || ''}
                      onChange={(e) => setConfig({ ...config, aliyun_tts_code: e.target.value })}
                      placeholder="TTS_12345678"
                      className="mt-1 text-xs font-mono"
                    />
                  </div>
                </div>
              </div>
            )}

            {config.provider === 'tencent' && (
              <div className="space-y-3 pt-2 border-t border-border">
                <span className="font-semibold text-text block">
                  {tr('腾讯云语音参数', 'Tencent Settings')}
                </span>
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <Label className="text-xs">SecretId</Label>
                    <Input
                      value={config.tencent_secret_id || ''}
                      onChange={(e) => setConfig({ ...config, tencent_secret_id: e.target.value })}
                      placeholder="AKID..."
                      className="mt-1 text-xs font-mono"
                    />
                  </div>
                  <div>
                    <Label className="text-xs">SecretKey</Label>
                    <Input
                      type="password"
                      value={config.tencent_secret_key || ''}
                      onChange={(e) => setConfig({ ...config, tencent_secret_key: e.target.value })}
                      placeholder="••••••••••••"
                      className="mt-1 text-xs font-mono"
                    />
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <Label className="text-xs">SdkAppId</Label>
                    <Input
                      value={config.tencent_sdk_app_id || ''}
                      onChange={(e) => setConfig({ ...config, tencent_sdk_app_id: e.target.value })}
                      placeholder="1400000000"
                      className="mt-1 text-xs font-mono"
                    />
                  </div>
                  <div>
                    <Label className="text-xs">{tr('模板 ID', 'Template ID')}</Label>
                    <Input
                      value={config.tencent_template_id || ''}
                      onChange={(e) => setConfig({ ...config, tencent_template_id: e.target.value })}
                      placeholder="123456"
                      className="mt-1 text-xs font-mono"
                    />
                  </div>
                </div>
              </div>
            )}
          </div>
        </Card>

        {/* Test Voice Call Simulator */}
        <Card className="p-5 space-y-4">
          <div className="flex items-center gap-2 border-b border-border pb-3">
            <PhoneCall size={16} className="text-indigo-500" />
            <h3 className="text-xs font-semibold text-text uppercase tracking-wider">
              {tr('在线语音外呼实测', 'Voice Call Simulator')}
            </h3>
          </div>

          <p className="text-xs text-text-muted">
            {tr(
              '模拟生成一起紧急生产告警外呼任务，验证语音 TTS 转换与电话接入逻辑。',
              'Simulate an urgent production alert voice call to verify TTS conversion and telephone delivery.'
            )}
          </p>

          <div className="space-y-3 text-xs">
            <div>
              <Label className="text-xs">{tr('测试目标手机号', 'Target Phone Number')} *</Label>
              <Input
                value={testPhone}
                onChange={(e) => setTestPhone(e.target.value)}
                placeholder="13800000001"
                className="mt-1 text-xs font-mono"
              />
            </div>

            <div>
              <Label className="text-xs">{tr('模拟告警标题（TTS 播报内容）', 'Alert Title for TTS')}</Label>
              <Input
                value={testTitle}
                onChange={(e) => setTestTitle(e.target.value)}
                placeholder={tr('订单支付服务超时告警', 'Order payment service timeout alert')}
                className="mt-1 text-xs"
              />
            </div>

            <Button
              size="sm"
              variant="primary"
              onClick={handleTestCall}
              disabled={calling}
              className="w-full"
            >
              <PhoneCall size={14} className="mr-1.5" />
              {calling ? tr('外呼拨打中...', 'Calling...') : tr('发起在线语音外呼测试', 'Trigger Voice Call')}
            </Button>

            {callResult && (
              <div className="mt-4 rounded-lg border border-border bg-bg p-3.5 space-y-2 font-mono text-xs">
                <div className="flex items-center justify-between">
                  <span className="text-text-muted">{tr('呼叫流水 ID', 'Call ID')}:</span>
                  <span className="text-text font-bold">{callResult.call_id}</span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-text-muted">{tr('服务提供商', 'Provider')}:</span>
                  <span className="text-indigo-600 dark:text-indigo-400 uppercase font-semibold">
                    {callResult.provider}
                  </span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-text-muted">{tr('呼叫状态', 'Status')}:</span>
                  <Chip tone="success" dense>
                    {callResult.status}
                  </Chip>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-text-muted">{tr('DTMF 确认按键', 'DTMF Code')}:</span>
                  <span className="text-emerald-600 dark:text-emerald-400 font-bold">
                    {tr(`按键 '${callResult.dtmf_code || '1'}'（已确认认领接单）`, `Key '${callResult.dtmf_code || '1'}' (Acknowledged)`)}
                  </span>
                </div>
                <div className="border-t border-border pt-2 text-[11px] text-text-muted">
                  <span className="block font-sans text-text mb-0.5">{tr('TTS 播报内容：', 'TTS Content: ')}</span>
                  <span className="font-sans italic text-text-muted">{callResult.tts_content}</span>
                </div>
              </div>
            )}
          </div>
        </Card>
      </div>
      {dialog}
    </div>
  );
}
