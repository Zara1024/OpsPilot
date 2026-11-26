import { useState } from 'react';
import {
  Plus,
  Copy,
  Check,
  Activity,
  BookOpen,
  Edit2,
  RotateCw,
  Trash2,
} from 'lucide-react';
import {
  Button,
  Card,
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
  createHeartbeat,
  updateHeartbeat,
  deleteHeartbeat,
  rotateHeartbeatToken,
  manualPingHeartbeat,
  type Heartbeat,
} from '@/api/alerts';
import { buildHeartbeatSnippet, resolveFullEndpointUrl } from './ingestGuides';

interface HeartbeatsTabProps {
  heartbeats: Heartbeat[];
  loading: boolean;
  onChanged: () => void;
}

export function HeartbeatsTab({ heartbeats, loading, onChanged }: HeartbeatsTabProps) {
  const { tr, locale } = useI18n();
  const { isAdmin } = usePermissions();
  const { confirmAction, alertAction, dialog } = useDialogs();

  const [copiedId, setCopiedId] = useState<string | null>(null);
  const [pingingId, setPingingId] = useState<number | null>(null);
  const [feedback, setFeedback] = useState<string | null>(null);

  // Modal state
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<Heartbeat | null>(null);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [intervalSec, setIntervalSec] = useState(60);
  const [graceSec, setGraceSec] = useState(120);
  const [severity, setSeverity] = useState('critical');
  const [labelsText, setLabelsText] = useState('service=monitoring');
  const [enabled, setEnabled] = useState(true);
  const [saving, setSaving] = useState(false);

  const [guideHB, setGuideHB] = useState<Heartbeat | null>(null);

  const handleCopy = async (key: string, val: string) => {
    try {
      await navigator.clipboard?.writeText(val);
      setCopiedId(key);
      setTimeout(() => setCopiedId(null), 2000);
    } catch {
      await alertAction(tr('复制失败，请手动选中复制', 'Copy failed, please copy manually'));
    }
  };

  const parseLabels = (raw: string): Record<string, string> => {
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
    setIntervalSec(60);
    setGraceSec(120);
    setSeverity('critical');
    setLabelsText('service=monitoring');
    setEnabled(true);
    setFormOpen(true);
  };

  const openEdit = (hb: Heartbeat) => {
    setEditing(hb);
    setName(hb.name);
    setDescription(hb.description || '');
    setIntervalSec(hb.interval_seconds || 60);
    setGraceSec(hb.grace_seconds || 120);
    setSeverity(hb.severity || 'critical');
    setLabelsText(
      Object.entries(hb.labels || {})
        .map(([k, v]) => `${k}=${v}`)
        .join(', '),
    );
    setEnabled(hb.enabled);
    setFormOpen(true);
  };

  const handleSave = async () => {
    if (!isAdmin || saving) return;
    if (!name.trim()) {
      await alertAction(tr('请输入心跳监控名称', 'Heartbeat name is required'));
      return;
    }
    setSaving(true);
    try {
      const payload = {
        name: name.trim(),
        description: description.trim(),
        interval_seconds: Math.max(10, Number(intervalSec) || 60),
        grace_seconds: Math.max(10, Number(graceSec) || 120),
        severity,
        labels: parseLabels(labelsText),
        enabled,
      };
      if (editing) {
        await updateHeartbeat(editing.id, payload);
      } else {
        await createHeartbeat(payload);
      }
      setFormOpen(false);
      onChanged();
    } catch (e: unknown) {
      await alertAction(e instanceof Error ? e.message : tr('保存心跳监控失败', 'Failed to save heartbeat'));
    } finally {
      setSaving(false);
    }
  };

  const handleManualPing = async (hb: Heartbeat) => {
    if (!isAdmin || pingingId) return;
    setPingingId(hb.id);
    setFeedback(null);
    try {
      const res = await manualPingHeartbeat(hb.id);
      setFeedback(
        res.auto_resolved
          ? tr(`已向「${hb.name}」上报心跳，并自动恢复关联失联告警。`, `Pinged "${hb.name}" and auto-resolved active incident.`)
          : tr(`已向「${hb.name}」上报心跳（当前状态：正常监听中）。`, `Pinged "${hb.name}" (state: healthy).`),
      );
      onChanged();
    } catch (e: unknown) {
      await alertAction(e instanceof Error ? e.message : tr('心跳上报失败', 'Ping failed'));
    } finally {
      setPingingId(null);
    }
  };

  const handleRotateToken = async (hb: Heartbeat) => {
    if (!isAdmin) return;
    const ok = await confirmAction(
      tr(
        `确认轮换「${hb.name}」的 Ping Token 吗？旧 Ping 地址将立即失效。`,
        `Rotate ping token for "${hb.name}"? Previous URLs will stop working.`,
      ),
    );
    if (!ok) return;
    try {
      await rotateHeartbeatToken(hb.id);
      onChanged();
    } catch (e: unknown) {
      await alertAction(e instanceof Error ? e.message : tr('轮换 Token 失败', 'Failed to rotate token'));
    }
  };

  const handleDelete = async (hb: Heartbeat) => {
    if (!isAdmin) return;
    const ok = await confirmAction(
      tr(`确认删除心跳监控「${hb.name}」吗？`, `Delete heartbeat "${hb.name}"?`),
    );
    if (!ok) return;
    try {
      await deleteHeartbeat(hb.id);
      onChanged();
    } catch (e: unknown) {
      await alertAction(e instanceof Error ? e.message : tr('删除心跳监控失败', 'Failed to delete heartbeat'));
    }
  };

  const renderStateChip = (hb: Heartbeat) => {
    switch (hb.state) {
      case 'healthy':
        return <Chip tone="success" dense className="whitespace-nowrap">{tr('正常', 'Healthy')}</Chip>;
      case 'alerting':
        return <Chip tone="danger" dense className="whitespace-nowrap">{tr('失联告警中', 'Alerting')}</Chip>;
      case 'paused':
        return <Chip tone="default" dense className="whitespace-nowrap">{tr('已暂停', 'Paused')}</Chip>;
      default:
        return <Chip tone="warning" dense className="whitespace-nowrap">{tr('等待首次心跳', 'Waiting First Ping')}</Chip>;
    }
  };

  return (
    <div className="space-y-4">
      {dialog}

      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-xs text-text-muted">
          {tr(
            '适用于 Prometheus Watchdog、定时备份任务等场景；超期未收到周期心跳 Ping 时自动触发失联告警并唤醒值班人。',
            'Dead-man switches for Prometheus Watchdog and cron jobs; alerts if expected pings stop arriving.',
          )}
        </p>
        {isAdmin && (
          <Button size="sm" variant="primary" onClick={openCreate}>
            <Plus size={14} className="mr-1.5" />
            {tr('新建心跳监控', 'New Heartbeat')}
          </Button>
        )}
      </div>

      {feedback && (
        <div
          role="status"
          className="flex items-center justify-between gap-3 rounded-lg border border-emerald-500/30 bg-emerald-500/10 px-3.5 py-2.5 text-xs text-emerald-700 dark:text-emerald-300"
        >
          <span>{feedback}</span>
          <Button size="sm" variant="subtle" onClick={() => setFeedback(null)}>
            {tr('收起', 'Dismiss')}
          </Button>
        </div>
      )}

      {!loading && heartbeats.length === 0 ? (
        <EmptyStateCard
          icon={Activity}
          title={tr('尚未创建死信心跳监控', 'No Heartbeat Monitors Yet')}
          hint={tr(
            '创建后将生成专属 Ping URL，并在首次收到心跳后启动超时失联检测。',
            'Create a heartbeat to generate a dedicated Ping URL and activate overdue monitoring on first ping.',
          )}
          action={
            isAdmin ? (
              <Button size="sm" variant="primary" onClick={openCreate}>
                <Plus size={14} className="mr-1.5" />
                {tr('新建心跳监控', 'New Heartbeat')}
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
                  <th>{tr('心跳名称与周期', 'Heartbeat & Interval')}</th>
                  <th>{tr('Ping 上报地址', 'Ping URL')}</th>
                  <th>{tr('状态与最近上报', 'State & Last Ping')}</th>
                  <th className="whitespace-nowrap">{tr('操作', 'Actions')}</th>
                </tr>
              </thead>
              <tbody>
                {heartbeats.map((hb) => {
                  const fullPing = resolveFullEndpointUrl(hb.ping_url);
                  return (
                    <tr key={hb.id}>
                      <td className="min-w-[220px] align-top">
                        <div className="flex flex-wrap items-center gap-1.5">
                          <span className="font-medium text-text">{hb.name}</span>
                          {renderStateChip(hb)}
                        </div>
                        {hb.description && (
                          <div className="mt-1 text-[11px] text-text-muted">{hb.description}</div>
                        )}
                        <div className="mt-1 text-[11px] text-text-faint">
                          {tr(
                            `期望每 ${hb.interval_seconds}s 上报 · 宽限 ${hb.grace_seconds}s · 级别 ${hb.severity}`,
                            `Every ${hb.interval_seconds}s · Grace ${hb.grace_seconds}s · ${hb.severity}`,
                          )}
                        </div>
                      </td>

                      <td className="max-w-[270px] align-top">
                        <div className="flex items-center gap-1.5">
                          <code className="max-w-[230px] truncate rounded border border-border bg-bg px-2 py-1 font-mono text-[11px] text-text">
                            {fullPing}
                          </code>
                          {isAdmin && (
                            <Button
                              size="sm"
                              variant="outline"
                              onClick={() => void handleCopy(`hb:${hb.id}`, fullPing)}
                              aria-label={tr('复制 Ping 地址', 'Copy ping URL')}
                              title={tr('复制 Ping 地址', 'Copy ping URL')}
                            >
                              {copiedId === `hb:${hb.id}` ? <Check size={12} /> : <Copy size={12} />}
                            </Button>
                          )}
                        </div>
                      </td>

                      <td className="align-top whitespace-nowrap">
                        <div className="text-[11px] text-text-muted">
                          {tr('累计 Ping:', 'Pings:')} <span className="font-mono text-text">{hb.ping_count}</span>
                        </div>
                        <div className="mt-0.5 text-[11px] text-text-faint">
                          {hb.last_ping_at
                            ? `${fullDateTime(hb.last_ping_at)} (${hb.last_ping_ip || '-'})`
                            : tr('等待首次上报激活', 'Waiting for first ping')}
                        </div>
                      </td>

                      <td className="w-px whitespace-nowrap align-top">
                        <div className="flex items-center gap-1">
                          {isAdmin && (
                            <Button
                              size="sm"
                              variant="outline"
                              onClick={() => void handleManualPing(hb)}
                              disabled={pingingId === hb.id}
                            >
                              <Activity size={12} className="mr-1 text-emerald-500" />
                              {tr('手动 Ping', 'Ping Now')}
                            </Button>
                          )}
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => setGuideHB(hb)}
                            aria-label={tr('查看接入命令', 'Show integration snippet')}
                            title={tr('查看接入命令', 'Show integration snippet')}
                          >
                            <BookOpen size={12} />
                          </Button>
                          {isAdmin && (
                            <>
                              <Button
                                size="sm"
                                variant="outline"
                                onClick={() => openEdit(hb)}
                                aria-label={tr('编辑心跳', 'Edit heartbeat')}
                                title={tr('编辑心跳', 'Edit heartbeat')}
                              >
                                <Edit2 size={12} />
                              </Button>
                              <Button
                                size="sm"
                                variant="outline"
                                onClick={() => void handleRotateToken(hb)}
                                aria-label={tr('轮换 Ping Token', 'Rotate ping token')}
                                title={tr('轮换 Ping Token', 'Rotate ping token')}
                              >
                                <RotateCw size={12} />
                              </Button>
                              <Button
                                size="sm"
                                variant="dangerGhost"
                                onClick={() => void handleDelete(hb)}
                                aria-label={tr('删除心跳', 'Delete heartbeat')}
                                title={tr('删除心跳', 'Delete heartbeat')}
                              >
                                <Trash2 size={12} />
                              </Button>
                            </>
                          )}
                        </div>
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
        title={editing ? tr('编辑死信心跳', 'Edit Heartbeat') : tr('新建死信心跳监控', 'New Heartbeat Monitor')}
        size="md"
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
          <div>
            <Label htmlFor="hb-name">{tr('名称', 'Name')} *</Label>
            <Input
              id="hb-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={tr('例如：Prometheus Watchdog', 'e.g. Prometheus Watchdog')}
              className="mt-1"
            />
          </div>
          <div>
            <Label htmlFor="hb-desc">{tr('说明', 'Description')}</Label>
            <Input
              id="hb-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              className="mt-1"
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <Label htmlFor="hb-interval">{tr('期望周期 (秒)', 'Interval (s)')}</Label>
              <Input
                id="hb-interval"
                type="number"
                min={10}
                value={intervalSec}
                onChange={(e) => setIntervalSec(Number(e.target.value))}
                className="mt-1"
              />
            </div>
            <div>
              <Label htmlFor="hb-grace">{tr('宽限期 (秒)', 'Grace Period (s)')}</Label>
              <Input
                id="hb-grace"
                type="number"
                min={10}
                value={graceSec}
                onChange={(e) => setGraceSec(Number(e.target.value))}
                className="mt-1"
              />
            </div>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <Label htmlFor="hb-severity">{tr('失联告警级别', 'Severity')}</Label>
              <Select
                id="hb-severity"
                label={tr('失联告警级别', 'Severity')}
                value={severity}
                onValueChange={setSeverity}
                options={[
                  { value: 'critical', label: 'Critical' },
                  { value: 'warning', label: 'Warning' },
                  { value: 'info', label: 'Info' },
                ]}
                className="mt-1 w-full"
              />
            </div>
            <div>
              <Label htmlFor="hb-labels">{tr('附加标签', 'Labels')}</Label>
              <Input
                id="hb-labels"
                value={labelsText}
                onChange={(e) => setLabelsText(e.target.value)}
                placeholder="service=monitoring"
                className="mt-1 font-mono"
              />
            </div>
          </div>
          <div className="flex items-center gap-2 pt-1">
            <Switch checked={enabled} onCheckedChange={setEnabled} />
            <span className="text-text">{tr('启用心跳超期监控', 'Monitor enabled')}</span>
          </div>
        </div>
      </Modal>

      {/* Heartbeat Snippet Modal */}
      <Modal
        open={guideHB !== null}
        onClose={() => setGuideHB(null)}
        title={guideHB ? `${guideHB.name} — ${tr('心跳接入示例', 'Heartbeat Ping Snippet')}` : ''}
        size="lg"
        footer={
          <Button size="sm" variant="primary" onClick={() => setGuideHB(null)}>
            {tr('关闭', 'Close')}
          </Button>
        }
      >
        {guideHB && (
          <div className="space-y-3 text-xs">
            <Label>{tr('Ping 命令示例', 'Ping Command Examples')}</Label>
            <Textarea
              readOnly
              rows={8}
              value={buildHeartbeatSnippet(guideHB.ping_url, locale === 'en-US' ? 'en' : 'zh')}
              className="font-mono text-[11px]"
            />
          </div>
        )}
      </Modal>
    </div>
  );
}
