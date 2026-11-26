import { useState, useMemo } from 'react';
import {
  Plus,
  Copy,
  Check,
  BookOpen,
  Play,
  CheckCircle2,
  Eye,
  Edit2,
  RotateCw,
  Trash2,
  ShieldCheck,
  AlertTriangle,
} from 'lucide-react';
import {
  Button,
  Card,
  Checkbox,
  Chip,
  EmptyStateCard,
  Input,
  Label,
  Select,
  Switch,
  Textarea,
} from '@/components/ui';
import { useDialogs } from '@/components/ui/useDialogs';
import { Modal } from '@/components/Modal';
import { useI18n } from '@/i18n/locale';
import { fullDateTime } from '@/lib/format';
import { usePermissions } from '@/store/me';
import {
  createIngestSource,
  updateIngestSource,
  deleteIngestSource,
  rotateIngestSourceToken,
  testIngestSource,
  type IngestProvider,
  type IngestSource,
  type IngestTestResult,
} from '@/api/alerts';
import {
  INGEST_PROVIDERS,
  buildProviderSnippet,
  resolveFullEndpointUrl,
} from './ingestGuides';

interface IngestSourcesTabProps {
  sources: IngestSource[];
  loading: boolean;
  onChanged: () => void;
}

export function IngestSourcesTab({ sources, loading, onChanged }: IngestSourcesTabProps) {
  const { tr, locale } = useI18n();
  const { isAdmin } = usePermissions();
  const { confirmAction, alertAction, dialog } = useDialogs();

  const [copiedId, setCopiedId] = useState<string | null>(null);
  const [testingKey, setTestingKey] = useState<string | null>(null);
  const [testBanner, setTestBanner] = useState<{ ok: boolean; text: string } | null>(null);

  // Create / Edit modal state
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<IngestSource | null>(null);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [provider, setProvider] = useState<IngestProvider>('alertmanager');
  const [defaultSeverity, setDefaultSeverity] = useState('warning');
  const [labelsText, setLabelsText] = useState('env=prod');
  const [autoResolve, setAutoResolve] = useState(true);
  const [enabled, setEnabled] = useState(true);
  const [rateLimit, setRateLimit] = useState(120);
  const [signingSecret, setSigningSecret] = useState('');
  const [clearSecret, setClearSecret] = useState(false);
  const [saving, setSaving] = useState(false);

  // Guide & Payload Preview modals
  const [guideSource, setGuideSource] = useState<IngestSource | null>(null);
  const [previewSource, setPreviewSource] = useState<IngestSource | null>(null);

  const providerOptions = useMemo(
    () =>
      INGEST_PROVIDERS.map((p) => ({
        value: p.key,
        label: tr(p.labelZh, p.labelEn),
      })),
    [tr],
  );

  const getProviderLabel = (key: IngestProvider) => {
    const meta = INGEST_PROVIDERS.find((p) => p.key === key);
    return meta ? tr(meta.labelZh, meta.labelEn) : key;
  };

  const handleCopy = async (key: string, text: string) => {
    try {
      await navigator.clipboard?.writeText(text);
      setCopiedId(key);
      setTimeout(() => setCopiedId(null), 2000);
    } catch {
      await alertAction(tr('复制失败，请手动选中复制', 'Copy failed, please copy manually'));
    }
  };

  const formatLabelsInput = (labels?: Record<string, string>) => {
    if (!labels) return '';
    return Object.entries(labels)
      .filter(([k, v]) => k && v)
      .map(([k, v]) => `${k}=${v}`)
      .join(', ');
  };

  const parseLabelsInput = (raw: string): Record<string, string> => {
    const out: Record<string, string> = {};
    for (const part of raw.split(',')) {
      const idx = part.indexOf('=');
      if (idx <= 0) continue;
      const k = part.slice(0, idx).trim();
      const v = part.slice(idx + 1).trim();
      if (k && v) out[k] = v;
    }
    return out;
  };

  const openCreate = () => {
    setEditing(null);
    setName('');
    setDescription('');
    setProvider('alertmanager');
    setDefaultSeverity('warning');
    setLabelsText('env=prod');
    setAutoResolve(true);
    setEnabled(true);
    setRateLimit(120);
    setSigningSecret('');
    setClearSecret(false);
    setFormOpen(true);
  };

  const openEdit = (src: IngestSource) => {
    setEditing(src);
    setName(src.name);
    setDescription(src.description || '');
    setProvider(src.provider);
    setDefaultSeverity(src.default_severity || 'warning');
    setLabelsText(formatLabelsInput(src.default_labels));
    setAutoResolve(src.auto_resolve);
    setEnabled(src.enabled);
    setRateLimit(src.rate_limit_per_min || 120);
    setSigningSecret('');
    setClearSecret(false);
    setFormOpen(true);
  };

  const handleSave = async () => {
    if (!isAdmin || saving) return;
    if (!name.trim()) {
      await alertAction(tr('请输入接入源名称', 'Ingest source name is required'));
      return;
    }
    setSaving(true);
    try {
      const payload = {
        name: name.trim(),
        description: description.trim(),
        provider,
        default_severity: defaultSeverity,
        default_labels: parseLabelsInput(labelsText),
        auto_resolve: autoResolve,
        enabled,
        rate_limit_per_min: Number(rateLimit) || 120,
        signing_secret: signingSecret.trim() || undefined,
        clear_signing_secret: clearSecret || undefined,
      };
      if (editing) {
        await updateIngestSource(editing.id, payload);
      } else {
        await createIngestSource(payload);
      }
      setFormOpen(false);
      onChanged();
    } catch (e: unknown) {
      await alertAction(e instanceof Error ? e.message : tr('保存接入源失败', 'Failed to save ingest source'));
    } finally {
      setSaving(false);
    }
  };

  const handleRotateToken = async (src: IngestSource) => {
    if (!isAdmin) return;
    const ok = await confirmAction(
      tr(
        `确认轮换「${src.name}」的 Webhook Token 吗？旧地址将立即失效，请同步更新上游监控系统。`,
        `Rotate webhook token for "${src.name}"? Existing URLs will stop working immediately.`,
      ),
    );
    if (!ok) return;
    try {
      await rotateIngestSourceToken(src.id);
      onChanged();
    } catch (e: unknown) {
      await alertAction(e instanceof Error ? e.message : tr('轮换 Token 失败', 'Failed to rotate token'));
    }
  };

  const handleDelete = async (src: IngestSource) => {
    if (!isAdmin) return;
    const ok = await confirmAction(
      tr(`确认删除外部接入源「${src.name}」吗？`, `Delete ingest source "${src.name}"?`),
    );
    if (!ok) return;
    try {
      await deleteIngestSource(src.id);
      onChanged();
    } catch (e: unknown) {
      await alertAction(e instanceof Error ? e.message : tr('删除接入源失败', 'Failed to delete source'));
    }
  };

  const handleTest = async (src: IngestSource, action: 'fire' | 'resolve') => {
    if (!isAdmin || testingKey) return;
    const key = `${src.id}:${action}`;
    setTestingKey(key);
    setTestBanner(null);
    try {
      const res: IngestTestResult = await testIngestSource(src.id, action);
      setTestBanner({
        ok: true,
        text: tr(
          `[${src.name}] 模拟${action === 'fire' ? '触发' : '恢复'}完成：${res.message}`,
          `[${src.name}] Simulated ${action} complete: ${res.message}`,
        ),
      });
      onChanged();
    } catch (e: unknown) {
      setTestBanner({
        ok: false,
        text: e instanceof Error ? e.message : tr('模拟告警失败', 'Simulated test failed'),
      });
    } finally {
      setTestingKey(null);
    }
  };

  const guideSnippet = useMemo(() => {
    if (!guideSource) return null;
    return buildProviderSnippet(guideSource.provider, guideSource.webhook_url, locale === 'en-US' ? 'en' : 'zh');
  }, [guideSource, locale]);

  return (
    <div className="space-y-4">
      {dialog}

      {/* Top Section Header */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="text-xs text-text-muted">
            {tr(
              '接入外部监控系统推送的 Webhook 告警，自动标准化并流转至告警中心、AI 初诊与 On-Call 值班升级链。',
              'Ingest external monitoring webhooks into OpsPilot incidents, AI triage, and On-Call escalation.',
            )}
          </p>
        </div>
        {isAdmin && (
          <Button size="sm" variant="primary" onClick={openCreate}>
            <Plus size={14} className="mr-1.5" />
            {tr('新建接入源', 'New Ingest Source')}
          </Button>
        )}
      </div>

      {testBanner && (
        <div
          role="status"
          className={`flex items-center justify-between gap-3 rounded-lg border px-3.5 py-2.5 text-xs ${
            testBanner.ok
              ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
              : 'border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-300'
          }`}
        >
          <span className="min-w-0 break-words">{testBanner.text}</span>
          <Button size="sm" variant="subtle" onClick={() => setTestBanner(null)}>
            {tr('收起', 'Dismiss')}
          </Button>
        </div>
      )}

      {!loading && sources.length === 0 ? (
        <EmptyStateCard
          icon={AlertTriangle}
          title={tr('尚未配置外部告警接入源', 'No External Ingest Sources Yet')}
          hint={tr(
            '支持 Alertmanager、Grafana、阿里云/腾讯云/华为云、Zabbix 与通用 Webhook 接入。',
            'Connect Alertmanager, Grafana, Aliyun, Tencent, Huawei Cloud, Zabbix, or generic webhooks.',
          )}
          action={
            isAdmin ? (
              <Button size="sm" variant="primary" onClick={openCreate}>
                <Plus size={14} className="mr-1.5" />
                {tr('新建接入源', 'New Ingest Source')}
              </Button>
            ) : undefined
          }
        />
      ) : (
        <Card className="overflow-hidden">
          <div className="overflow-x-auto">
            <table className="og-resource-table w-full text-left text-xs">
              <thead>
                <tr>
                  <th>{tr('接入源与协议', 'Source & Protocol')}</th>
                  <th>{tr('Webhook 地址与标签', 'Webhook URL & Labels')}</th>
                  <th>{tr('状态与统计', 'Status & Telemetry')}</th>
                  <th className="whitespace-nowrap">{tr('操作', 'Actions')}</th>
                </tr>
              </thead>
              <tbody>
                {sources.map((src) => {
                  const fullUrl = resolveFullEndpointUrl(src.webhook_url);
                  return (
                    <tr key={src.id}>
                      <td className="min-w-[220px] align-top">
                        <div className="flex flex-wrap items-center gap-1.5">
                          <span className="font-medium text-text">{src.name}</span>
                          <Chip tone="accent" dense className="whitespace-nowrap">
                            {getProviderLabel(src.provider)}
                          </Chip>
                          {src.signing_secret_configured && (
                            <Chip tone="info" dense className="whitespace-nowrap">
                              <ShieldCheck size={10} className="mr-0.5 shrink-0" />
                              HMAC
                            </Chip>
                          )}
                        </div>
                        {src.description && (
                          <div className="mt-1 text-[11px] text-text-muted">{src.description}</div>
                        )}
                        <div className="mt-1 flex flex-wrap items-center gap-1.5 text-[11px] text-text-faint">
                          <span>
                            {tr('默认等级:', 'Default:')} {src.default_severity}
                          </span>
                          <span>·</span>
                          <span>
                            {src.auto_resolve
                              ? tr('自动恢复关单', 'Auto-resolve on')
                              : tr('仅触发不自动关单', 'Auto-resolve off')}
                          </span>
                          <span>·</span>
                          <span>{src.rate_limit_per_min}/min</span>
                        </div>
                      </td>

                      <td className="max-w-[240px] align-top">
                        <div className="flex items-center gap-1.5">
                          <code className="max-w-[200px] truncate rounded border border-border bg-bg px-2 py-1 font-mono text-[11px] text-text">
                            {fullUrl}
                          </code>
                          {isAdmin && (
                            <Button
                              size="sm"
                              variant="outline"
                              onClick={() => void handleCopy(`url:${src.id}`, fullUrl)}
                              aria-label={tr('复制 Webhook 地址', 'Copy webhook URL')}
                              title={tr('复制 Webhook 地址', 'Copy webhook URL')}
                            >
                              {copiedId === `url:${src.id}` ? <Check size={12} /> : <Copy size={12} />}
                            </Button>
                          )}
                        </div>
                        {Object.keys(src.default_labels || {}).length > 0 && (
                          <div className="mt-1.5 flex flex-wrap gap-1">
                            {Object.entries(src.default_labels).map(([k, v]) => (
                              <span
                                key={k}
                                className="rounded border border-border bg-bg px-1.5 py-0.5 font-mono text-[10px] text-text-muted"
                              >
                                {k}={v}
                              </span>
                            ))}
                          </div>
                        )}
                      </td>

                      <td className="align-top whitespace-nowrap">
                        <div className="flex items-center gap-1.5">
                          <span
                            className={`h-2 w-2 rounded-full ${
                              !src.enabled
                                ? 'bg-text-faint'
                                : src.error_count > 0 && src.last_status !== 'processed'
                                  ? 'bg-amber-500'
                                  : 'bg-emerald-500'
                            }`}
                          />
                          <span className="text-text-muted">
                            {src.enabled ? tr('已启用', 'Enabled') : tr('已停用', 'Disabled')}
                          </span>
                        </div>
                        <div className="mt-1 text-[11px] text-text-faint">
                          {tr('接收', 'Received')}: <span className="font-mono text-text">{src.received_count}</span>
                          {' · '}
                          {tr('失败', 'Errors')}:{' '}
                          <span className={`font-mono ${src.error_count > 0 ? 'text-red-500' : 'text-text'}`}>
                            {src.error_count}
                          </span>
                        </div>
                        <div className="mt-0.5 text-[11px] text-text-faint">
                          {src.last_received_at
                            ? `${tr('最近:', 'Last:')} ${fullDateTime(src.last_received_at)}`
                            : tr('尚无入站记录', 'No deliveries yet')}
                        </div>
                        {src.subscribe_url && (
                          <div className="mt-1">
                            <Chip tone="warning" dense className="whitespace-nowrap">
                              {tr('待确认 SMN 订阅', 'SMN Confirm Ready')}
                            </Chip>
                          </div>
                        )}
                      </td>

                      <td className="w-px whitespace-nowrap align-top">
                        {isAdmin ? (
                          <div className="flex flex-col items-start gap-1.5">
                            <div className="flex items-center gap-1">
                              <Button
                                size="sm"
                                variant="outline"
                                onClick={() => setGuideSource(src)}
                              >
                                <BookOpen size={12} className="mr-1" />
                                {tr('接入向导', 'Guide')}
                              </Button>
                              <Button
                                size="sm"
                                variant="outline"
                                onClick={() => void handleTest(src, 'fire')}
                                disabled={testingKey === `${src.id}:fire`}
                                title={tr('向平台推送一条模拟触发告警', 'Simulate firing alert')}
                              >
                                <Play size={12} className="mr-1 text-indigo-500" />
                                {tr('模拟告警', 'Test Fire')}
                              </Button>
                              <Button
                                size="sm"
                                variant="outline"
                                onClick={() => void handleTest(src, 'resolve')}
                                disabled={testingKey === `${src.id}:resolve`}
                                title={tr('向平台推送一条模拟恢复事件', 'Simulate resolved alert')}
                              >
                                <CheckCircle2 size={12} className="mr-1 text-emerald-500" />
                                {tr('模拟恢复', 'Test Resolve')}
                              </Button>
                            </div>
                            <div className="flex items-center gap-1">
                              <Button
                                size="sm"
                                variant="outline"
                                onClick={() => setPreviewSource(src)}
                                aria-label={tr('查看最近载荷', 'Inspect last payload')}
                                title={tr('查看最近载荷', 'Inspect last payload')}
                              >
                                <Eye size={12} />
                              </Button>
                              <Button
                                size="sm"
                                variant="outline"
                                onClick={() => openEdit(src)}
                                aria-label={tr('编辑接入源', 'Edit source')}
                                title={tr('编辑接入源', 'Edit source')}
                              >
                                <Edit2 size={12} />
                              </Button>
                              <Button
                                size="sm"
                                variant="outline"
                                onClick={() => void handleRotateToken(src)}
                                aria-label={tr('轮换 Token', 'Rotate token')}
                                title={tr('轮换 Token', 'Rotate token')}
                              >
                                <RotateCw size={12} />
                              </Button>
                              <Button
                                size="sm"
                                variant="dangerGhost"
                                onClick={() => void handleDelete(src)}
                                aria-label={tr('删除接入源', 'Delete source')}
                                title={tr('删除接入源', 'Delete source')}
                              >
                                <Trash2 size={12} />
                              </Button>
                            </div>
                          </div>
                        ) : (
                          <div className="flex items-center gap-1">
                            <Button
                              size="sm"
                              variant="outline"
                              onClick={() => setGuideSource(src)}
                            >
                              <BookOpen size={12} className="mr-1" />
                              {tr('接入向导', 'Guide')}
                            </Button>
                            <Button
                              size="sm"
                              variant="outline"
                              onClick={() => setPreviewSource(src)}
                              aria-label={tr('查看最近载荷', 'Inspect last payload')}
                              title={tr('查看最近载荷', 'Inspect last payload')}
                            >
                              <Eye size={12} />
                            </Button>
                          </div>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </Card>
      )}

      {/* Create / Edit Modal */}
      <Modal
        open={formOpen}
        onClose={() => setFormOpen(false)}
        title={editing ? tr('编辑外部接入源', 'Edit Ingest Source') : tr('新建外部告警接入源', 'New Ingest Source')}
        size="lg"
        footer={
          <>
            <Button size="sm" variant="outline" onClick={() => setFormOpen(false)}>
              {tr('取消', 'Cancel')}
            </Button>
            <Button size="sm" variant="primary" onClick={() => void handleSave()} disabled={saving}>
              {saving ? tr('保存中…', 'Saving…') : tr('保存', 'Save')}
            </Button>
          </>
        }
      >
        <div className="space-y-4 text-xs">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <Label htmlFor="ingest-name">{tr('接入源名称', 'Source Name')} *</Label>
              <Input
                id="ingest-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={tr('例如：核心区 Zabbix 7', 'e.g. Core Zabbix 7')}
                className="mt-1"
              />
            </div>
            <div>
              <Label htmlFor="ingest-provider">{tr('监控协议', 'Provider Protocol')}</Label>
              <Select
                id="ingest-provider"
                label={tr('监控协议', 'Provider Protocol')}
                value={provider}
                onValueChange={(v) => setProvider(v as IngestProvider)}
                options={providerOptions}
                className="mt-1 w-full"
              />
            </div>
          </div>

          <div>
            <Label htmlFor="ingest-desc">{tr('备注说明', 'Description')}</Label>
            <Input
              id="ingest-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={tr('记录所属云账号、集群或负责人', 'Optional cloud account, cluster, or owner note')}
              className="mt-1"
            />
          </div>

          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <Label htmlFor="ingest-severity">{tr('默认严重级别', 'Default Severity')}</Label>
              <Select
                id="ingest-severity"
                label={tr('默认严重级别', 'Default Severity')}
                value={defaultSeverity}
                onValueChange={setDefaultSeverity}
                options={[
                  { value: 'critical', label: 'Critical' },
                  { value: 'warning', label: 'Warning' },
                  { value: 'info', label: 'Info' },
                ]}
                className="mt-1 w-full"
              />
            </div>
            <div>
              <Label htmlFor="ingest-ratelimit">{tr('单源限流 (请求/分钟)', 'Rate Limit (req/min)')}</Label>
              <Input
                id="ingest-ratelimit"
                type="number"
                min={1}
                max={5000}
                value={rateLimit}
                onChange={(e) => setRateLimit(Number(e.target.value))}
                className="mt-1"
              />
            </div>
          </div>

          <div>
            <Label htmlFor="ingest-labels">
              {tr('默认附加标签 (逗号分隔 key=value)', 'Default Labels (comma-separated key=value)')}
            </Label>
            <Input
              id="ingest-labels"
              value={labelsText}
              onChange={(e) => setLabelsText(e.target.value)}
              placeholder="env=prod, service=payment"
              className="mt-1 font-mono"
            />
            <p className="mt-1 text-[11px] text-text-faint">
              {tr(
                '这些标签会自动并入每一条入站告警，用于精准命中 On-Call 值班路由树。',
                'Merged into every incoming alert for On-Call label routing.',
              )}
            </p>
          </div>

          <div>
            <Label htmlFor="ingest-hmac">
              {tr('可选 HMAC-SHA256 签名密钥', 'Optional HMAC-SHA256 Signing Secret')}
            </Label>
            <Input
              id="ingest-hmac"
              type="password"
              value={signingSecret}
              onChange={(e) => setSigningSecret(e.target.value)}
              placeholder={
                editing?.signing_secret_configured
                  ? tr('已配置密钥；留空保持不变', 'Configured; leave blank to keep')
                  : tr('留空则仅校验 URL Token', 'Optional; verifies X-OpsPilot-Signature when set')
              }
              className="mt-1 font-mono"
            />
            {editing?.signing_secret_configured && (
              <div className="mt-2 flex items-center gap-2 text-[11px] text-text-muted">
                <Checkbox
                  id="ingest-clear-hmac"
                  checked={clearSecret}
                  onCheckedChange={(v) => setClearSecret(Boolean(v))}
                />
                <Label htmlFor="ingest-clear-hmac" className="cursor-pointer font-normal">
                  {tr('清除已配置的 HMAC 密钥', 'Clear existing HMAC secret')}
                </Label>
              </div>
            )}
          </div>

          <div className="flex flex-wrap items-center gap-6 pt-1">
            <div className="flex items-center gap-2">
              <Switch checked={autoResolve} onCheckedChange={setAutoResolve} />
              <span className="text-text">{tr('收到上游恢复事件时自动关单', 'Auto-resolve incidents on recovery')}</span>
            </div>
            <div className="flex items-center gap-2">
              <Switch checked={enabled} onCheckedChange={setEnabled} />
              <span className="text-text">{tr('启用该接入源', 'Source enabled')}</span>
            </div>
          </div>
        </div>
      </Modal>

      {/* Protocol Integration Guide Modal */}
      <Modal
        open={guideSource !== null}
        onClose={() => setGuideSource(null)}
        title={guideSource ? `${guideSource.name} — ${tr('接入配置向导', 'Integration Guide')}` : ''}
        size="lg"
        resizable
        footer={
          <Button size="sm" variant="primary" onClick={() => setGuideSource(null)}>
            {tr('完成', 'Done')}
          </Button>
        }
      >
        {guideSource && guideSnippet && (
          <div className="space-y-4 text-xs">
            <div>
              <Label>{tr('专属 Webhook 接收地址', 'Dedicated Webhook URL')}</Label>
              <div className="mt-1 flex items-center gap-2">
                <Input
                  readOnly
                  value={resolveFullEndpointUrl(guideSource.webhook_url)}
                  className="flex-1 font-mono text-xs"
                />
                {isAdmin && (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => void handleCopy('guide-url', resolveFullEndpointUrl(guideSource.webhook_url))}
                  >
                    {copiedId === 'guide-url' ? <Check size={12} className="mr-1" /> : <Copy size={12} className="mr-1" />}
                    {tr('复制', 'Copy')}
                  </Button>
                )}
              </div>
            </div>

            {guideSource.subscribe_url && (
              <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-amber-700 dark:text-amber-300">
                <div className="font-semibold">
                  {tr('检测到华为云 SMN 订阅确认链接', 'Huawei SMN Subscription Confirmation Detected')}
                </div>
                <div className="mt-1 flex items-center gap-2">
                  <Input readOnly value={guideSource.subscribe_url} className="flex-1 font-mono text-[11px]" />
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => void handleCopy('smn-url', guideSource.subscribe_url || '')}
                  >
                    {copiedId === 'smn-url' ? <Check size={12} /> : <Copy size={12} />}
                  </Button>
                </div>
              </div>
            )}

            <div>
              <div className="flex items-center justify-between">
                <Label>{guideSnippet.title}</Label>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => void handleCopy('guide-code', guideSnippet.code)}
                >
                  {copiedId === 'guide-code' ? <Check size={12} className="mr-1" /> : <Copy size={12} className="mr-1" />}
                  {tr('复制配置', 'Copy Snippet')}
                </Button>
              </div>
              <Textarea
                readOnly
                rows={guideSnippet.code.split('\n').length > 8 ? 14 : 6}
                value={guideSnippet.code}
                className="mt-1.5 font-mono text-[11px]"
              />
              {guideSnippet.note && (
                <p className="mt-1.5 text-[11px] text-text-muted">{guideSnippet.note}</p>
              )}
            </div>
          </div>
        )}
      </Modal>

      {/* Last Payload Preview Modal */}
      <Modal
        open={previewSource !== null}
        onClose={() => setPreviewSource(null)}
        title={previewSource ? `${previewSource.name} — ${tr('最近一次入站载荷', 'Last Payload Preview')}` : ''}
        size="lg"
        resizable
        footer={
          <Button size="sm" variant="outline" onClick={() => setPreviewSource(null)}>
            {tr('关闭', 'Close')}
          </Button>
        }
      >
        {previewSource && (
          <div className="space-y-3 text-xs">
            <div className="flex flex-wrap items-center gap-3 text-text-muted">
              <span>
                {tr('状态:', 'Status:')} <strong className="text-text">{previewSource.last_status || '-'}</strong>
              </span>
              {previewSource.last_received_at && (
                <span>
                  {tr('时间:', 'Time:')} {fullDateTime(previewSource.last_received_at)}
                </span>
              )}
            </div>
            {previewSource.last_error && (
              <div className="rounded border border-red-500/30 bg-red-500/10 p-2.5 text-red-700 dark:text-red-300">
                {previewSource.last_error}
              </div>
            )}
            <Textarea
              readOnly
              rows={12}
              value={
                previewSource.last_payload_preview ||
                tr('尚无入站载荷记录，点击「模拟告警」或等待上游推送。', 'No payload recorded yet.')
              }
              className="font-mono text-[11px]"
            />
          </div>
        )}
      </Modal>
    </div>
  );
}
