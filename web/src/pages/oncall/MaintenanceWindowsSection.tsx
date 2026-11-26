import { useState, useEffect, useCallback } from 'react';
import {
  ShieldOff,
  Plus,
  Trash2,
  XCircle,
  RefreshCw,
} from 'lucide-react';
import {
  Button,
  Card,
  Chip,
  EmptyState,
  Input,
  Label,
  Select,
  Textarea,
} from '@/components/ui';
import { useDialogs } from '@/components/ui/useDialogs';
import { Modal } from '@/components/Modal';
import { useI18n } from '@/i18n/locale';
import {
  listMaintenanceWindows,
  createMaintenanceWindow,
  cancelMaintenanceWindow,
  deleteMaintenanceWindow,
  type MaintenanceWindowItem,
  type OnCallSchedule,
  type LabelMatcher,
} from '@/api/oncall';

interface MaintenanceWindowsSectionProps {
  schedules: OnCallSchedule[];
  onChanged?: () => void;
}

function defaultLocalDatetime(offsetMinutes: number): string {
  const d = new Date(Date.now() + offsetMinutes * 60_000);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function MaintenanceWindowsSection({
  schedules,
  onChanged,
}: MaintenanceWindowsSectionProps) {
  const { tr } = useI18n();
  const { confirmAction, alertAction, dialog } = useDialogs();

  const [windows, setWindows] = useState<MaintenanceWindowItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [statusFilter, setStatusFilter] = useState('all');

  // Create modal state
  const [modalOpen, setModalOpen] = useState(false);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [scheduleId, setScheduleId] = useState<number>(0);
  const [action, setAction] = useState<'suppress_all' | 'suppress_voice'>('suppress_all');
  const [startsAt, setStartsAt] = useState(() => defaultLocalDatetime(0));
  const [endsAt, setEndsAt] = useState(() => defaultLocalDatetime(120));
  const [matchers, setMatchers] = useState<LabelMatcher[]>([]);
  const [saving, setSaving] = useState(false);

  const fetchWindows = useCallback(async () => {
    setLoading(true);
    try {
      const res = await listMaintenanceWindows(statusFilter);
      setWindows(res.items || []);
    } catch (e) {
      console.error('Failed to load maintenance windows:', e);
    } finally {
      setLoading(false);
    }
  }, [statusFilter]);

  useEffect(() => {
    void fetchWindows();
  }, [fetchWindows]);

  const handleOpenCreate = () => {
    setName('');
    setDescription('');
    setScheduleId(0);
    setAction('suppress_all');
    setStartsAt(defaultLocalDatetime(0));
    setEndsAt(defaultLocalDatetime(120));
    setMatchers([]);
    setModalOpen(true);
  };

  const handleCreate = async () => {
    if (!name.trim()) {
      await alertAction(tr('请输入维护窗口名称', 'Maintenance window name is required'));
      return;
    }
    const startDate = new Date(startsAt);
    const endDate = new Date(endsAt);
    if (Number.isNaN(startDate.getTime()) || Number.isNaN(endDate.getTime()) || endDate <= startDate) {
      await alertAction(tr('结束时间必须晚于开始时间', 'End time must be after start time'));
      return;
    }

    setSaving(true);
    try {
      const validMatchers = matchers.filter((m) => m.name.trim() !== '');
      await createMaintenanceWindow({
        name: name.trim(),
        description: description.trim(),
        schedule_id: scheduleId,
        action,
        matchers: validMatchers,
        starts_at: startDate.toISOString(),
        ends_at: endDate.toISOString(),
      });
      setModalOpen(false);
      await fetchWindows();
      onChanged?.();
    } catch (e: unknown) {
      await alertAction(e instanceof Error ? e.message : tr('创建维护窗口失败', 'Failed to create maintenance window'));
    } finally {
      setSaving(false);
    }
  };

  const handleCancel = async (mw: MaintenanceWindowItem) => {
    const ok = await confirmAction(
      tr(`确定提前终止维护窗口「${mw.name}」吗？终止后将立即恢复正常告警值班通知。`, `Cancel maintenance window "${mw.name}" early?`),
    );
    if (!ok) return;
    try {
      await cancelMaintenanceWindow(mw.id);
      await fetchWindows();
      onChanged?.();
    } catch (e: unknown) {
      await alertAction(e instanceof Error ? e.message : tr('终止维护窗口失败', 'Failed to cancel window'));
    }
  };

  const handleDelete = async (mw: MaintenanceWindowItem) => {
    const ok = await confirmAction(
      tr(`确认删除维护窗口记录「${mw.name}」吗？`, `Delete maintenance window "${mw.name}"?`),
    );
    if (!ok) return;
    try {
      await deleteMaintenanceWindow(mw.id);
      await fetchWindows();
      onChanged?.();
    } catch (e: unknown) {
      await alertAction(e instanceof Error ? e.message : tr('删除维护窗口失败', 'Failed to delete window'));
    }
  };

  const renderStatusChip = (status: MaintenanceWindowItem['runtime_status']) => {
    switch (status) {
      case 'running':
        return <Chip tone="warning" dense>{tr('生效中', 'Running')}</Chip>;
      case 'scheduled':
        return <Chip tone="info" dense>{tr('待生效', 'Scheduled')}</Chip>;
      case 'cancelled':
        return <Chip tone="default" dense>{tr('已终止', 'Cancelled')}</Chip>;
      default:
        return <Chip tone="default" dense>{tr('已结束', 'Expired')}</Chip>;
    }
  };

  return (
    <div className="space-y-4 pt-4 border-t border-border">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="flex items-center gap-2 min-w-0">
          <ShieldOff className="text-amber-500 shrink-0" size={18} />
          <div className="min-w-0">
            <h2 className="text-base font-semibold text-text">
              {tr('计划内维护窗口联动', 'Scheduled Maintenance Windows')}
            </h2>
            <p className="text-xs text-text-muted">
              {tr(
                '在变更发布或机房割接窗口内自动抑制全量派发或静默夜间语音电话，避免误叫醒与告警轰炸。',
                'Automatically suppress alert dispatches or mute voice calls during planned maintenance.'
              )}
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Select
            value={statusFilter}
            onValueChange={setStatusFilter}
            options={[
              { value: 'all', label: tr('全部状态', 'All Statuses') },
              { value: 'running', label: tr('生效中', 'Running') },
              { value: 'scheduled', label: tr('待生效', 'Scheduled') },
              { value: 'expired', label: tr('已结束', 'Expired') },
              { value: 'cancelled', label: tr('已终止', 'Cancelled') },
            ]}
            className="w-32 text-xs"
          />
          <Button
            size="sm"
            variant="ghost"
            onClick={fetchWindows}
            disabled={loading}
            aria-label={tr('刷新维护窗口列表', 'Refresh maintenance windows')}
          >
            <RefreshCw size={14} className={loading ? 'animate-spin' : ''} />
          </Button>
          <Button size="sm" variant="primary" onClick={handleOpenCreate}>
            <Plus size={14} className="mr-1.5" />
            {tr('新建维护窗口', 'New Maintenance Window')}
          </Button>
        </div>
      </div>

      {/* Windows Table Card */}
      <Card className="!p-0 overflow-hidden">
        {loading ? (
          <div className="flex h-40 items-center justify-center">
            <RefreshCw size={20} className="animate-spin text-indigo-500" />
          </div>
        ) : windows.length === 0 ? (
          <EmptyState
            icon={ShieldOff}
            title={tr('暂无计划内维护窗口', 'No Maintenance Windows Found')}
            hint={tr(
              '可点击右上角【新建维护窗口】，为变更期绑定全量抑制或语音静默策略。',
              'Click "New Maintenance Window" to configure planned suppression periods.'
            )}
            action={
              <Button size="sm" variant="primary" onClick={handleOpenCreate}>
                <Plus size={14} className="mr-1.5" />
                {tr('新建维护窗口', 'New Maintenance Window')}
              </Button>
            }
          />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs border-collapse text-text">
              <thead>
                <tr className="border-b border-border bg-bg/50 text-text-muted font-semibold">
                  <th className="py-2.5 px-4">{tr('窗口名称与说明', 'Window')}</th>
                  <th className="py-2.5 px-4">{tr('作用范围与匹配器', 'Scope & Matchers')}</th>
                  <th className="py-2.5 px-4">{tr('抑制动作', 'Action')}</th>
                  <th className="py-2.5 px-4">{tr('时间区间', 'Window Period')}</th>
                  <th className="py-2.5 px-4">{tr('状态与已抑制数', 'Status')}</th>
                  <th className="py-2.5 px-4 text-left">{tr('操作', 'Actions')}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {windows.map((mw) => (
                  <tr key={mw.id} className="hover:bg-bg/40 transition-colors">
                    <td className="py-2.5 px-4 min-w-[180px]">
                      <div className="font-semibold text-text">{mw.name}</div>
                      {mw.description && (
                        <div className="text-[11px] text-text-muted mt-0.5 break-words">{mw.description}</div>
                      )}
                    </td>
                    <td className="py-2.5 px-4">
                      <div className="font-medium text-text">{mw.schedule_name || tr('全局所有排班', 'All Schedules')}</div>
                      {mw.matchers && mw.matchers.length > 0 ? (
                        <div className="flex flex-wrap gap-1 mt-1">
                          {mw.matchers.map((m, i) => (
                            <span
                              key={i}
                              className="inline-flex rounded border border-border bg-bg px-1.5 py-0.5 font-mono text-[10px]"
                            >
                              {m.name} {m.operator} {m.value}
                            </span>
                          ))}
                        </div>
                      ) : (
                        <span className="text-[11px] text-text-faint">{tr('匹配范围全部告警', 'Matches all alerts')}</span>
                      )}
                    </td>
                    <td className="py-2.5 px-4 whitespace-nowrap">
                      <Chip tone={mw.action === 'suppress_all' ? 'warning' : 'accent'} dense>
                        {mw.action === 'suppress_all'
                          ? tr('全量抑制通知', 'Suppress All')
                          : tr('仅静默语音外呼', 'Mute Voice Only')}
                      </Chip>
                    </td>
                    <td className="py-2.5 px-4 font-mono text-[11px] text-text-muted whitespace-nowrap">
                      <div>{new Date(mw.starts_at).toLocaleString()}</div>
                      <div>~ {new Date(mw.ends_at).toLocaleString()}</div>
                    </td>
                    <td className="py-2.5 px-4 whitespace-nowrap">
                      <div className="flex items-center gap-1.5">
                        {renderStatusChip(mw.runtime_status)}
                        <Chip tone="info" dense>
                          {tr(`已抑制 ${mw.suppressed_count} 次`, `${mw.suppressed_count} suppressed`)}
                        </Chip>
                      </div>
                    </td>
                    <td className="py-2.5 px-4 text-left whitespace-nowrap">
                      <div className="flex items-center gap-1.5">
                        {(mw.runtime_status === 'running' || mw.runtime_status === 'scheduled') && (
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => handleCancel(mw)}
                          >
                            <XCircle size={12} className="mr-1" />
                            {tr('终止', 'Cancel')}
                          </Button>
                        )}
                        <Button
                          size="sm"
                          variant="dangerGhost"
                          onClick={() => handleDelete(mw)}
                          aria-label={tr('删除维护窗口', 'Delete maintenance window')}
                        >
                          <Trash2 size={12} />
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      {/* Create Maintenance Window Modal */}
      <Modal
        open={modalOpen}
        onClose={() => setModalOpen(false)}
        title={tr('新建计划内维护窗口', 'Create Maintenance Window')}
        size="md"
        footer={
          <div className="flex justify-end gap-2 w-full">
            <Button size="sm" variant="ghost" onClick={() => setModalOpen(false)}>
              {tr('取消', 'Cancel')}
            </Button>
            <Button size="sm" variant="primary" onClick={handleCreate} disabled={saving || !name.trim()}>
              {saving ? tr('创建中...', 'Creating...') : tr('确认创建', 'Create Window')}
            </Button>
          </div>
        }
      >
        <div className="space-y-3 py-1 text-xs">
          <div>
            <Label className="text-xs">{tr('维护窗口名称', 'Window Name')} *</Label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={tr('如：核心支付库凌晨升配维护', 'e.g. Payment DB upgrade window')}
              className="mt-1 text-xs"
            />
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div>
              <Label className="text-xs">{tr('关联排班范围', 'Schedule Scope')}</Label>
              <Select
                value={String(scheduleId)}
                onValueChange={(val) => setScheduleId(Number(val))}
                options={[
                  { value: '0', label: tr('全局（所有排班计划）', 'Global (All Schedules)') },
                  ...schedules.map((s) => ({ value: String(s.id), label: s.name })),
                ]}
                className="mt-1 w-full text-xs"
              />
            </div>
            <div>
              <Label className="text-xs">{tr('抑制策略模式', 'Suppression Policy')}</Label>
              <Select
                value={action}
                onValueChange={(val) => setAction(val as 'suppress_all' | 'suppress_voice')}
                options={[
                  { value: 'suppress_all', label: tr('全量抑制告警派发与升级', 'Suppress All Notifications') },
                  { value: 'suppress_voice', label: tr('仅静默语音外呼（保留 IM/邮件）', 'Mute Voice Calls Only') },
                ]}
                className="mt-1 w-full text-xs"
              />
            </div>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div>
              <Label className="text-xs">{tr('开始时间', 'Starts At')} *</Label>
              <Input
                type="datetime-local"
                value={startsAt}
                onChange={(e) => setStartsAt(e.target.value)}
                className="mt-1 text-xs font-mono"
              />
            </div>
            <div>
              <Label className="text-xs">{tr('结束时间', 'Ends At')} *</Label>
              <Input
                type="datetime-local"
                value={endsAt}
                onChange={(e) => setEndsAt(e.target.value)}
                className="mt-1 text-xs font-mono"
              />
            </div>
          </div>

          <div>
            <Label className="text-xs">{tr('变更说明（可选）', 'Description (Optional)')}</Label>
            <Textarea
              rows={2}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={tr('记录变更单号或影响范围...', 'Change ticket ID or impacted services...')}
              className="mt-1 text-xs"
            />
          </div>

          {/* Label Matchers */}
          <div className="pt-2 border-t border-border space-y-2">
            <div className="flex items-center justify-between">
              <Label className="text-xs">{tr('限定告警标签匹配（留空表示全部）', 'Label Matchers (Optional)')}</Label>
              <Button
                size="sm"
                variant="outline"
                type="button"
                onClick={() => setMatchers([...matchers, { name: 'service', operator: '=', value: '' }])}
              >
                <Plus size={12} className="mr-1" />
                {tr('添加标签', 'Add Matcher')}
              </Button>
            </div>
            {matchers.map((m, idx) => (
              <div key={idx} className="flex items-center gap-2">
                <Input
                  value={m.name}
                  onChange={(e) => {
                    const next = [...matchers];
                    next[idx].name = e.target.value;
                    setMatchers(next);
                  }}
                  placeholder="service"
                  className="w-28 text-xs font-mono"
                />
                <Select
                  value={m.operator}
                  onValueChange={(val) => {
                    const next = [...matchers];
                    next[idx].operator = val as LabelMatcher['operator'];
                    setMatchers(next);
                  }}
                  options={[
                    { value: '=', label: '=' },
                    { value: '!=', label: '!=' },
                    { value: '=~', label: '=~' },
                    { value: '!~', label: '!~' },
                  ]}
                  className="w-20 text-xs"
                />
                <Input
                  value={m.value}
                  onChange={(e) => {
                    const next = [...matchers];
                    next[idx].value = e.target.value;
                    setMatchers(next);
                  }}
                  placeholder="payment"
                  className="flex-1 text-xs font-mono"
                />
                <Button
                  size="sm"
                  variant="dangerGhost"
                  type="button"
                  onClick={() => setMatchers(matchers.filter((_, i) => i !== idx))}
                  aria-label={tr('移除匹配条件', 'Remove matcher')}
                >
                  <Trash2 size={12} />
                </Button>
              </div>
            ))}
          </div>
        </div>
      </Modal>
      {dialog}
    </div>
  );
}
