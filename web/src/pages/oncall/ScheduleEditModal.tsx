import { useState, useEffect, useMemo } from 'react';
import {
  CalendarDays,
  Clock,
  ShieldCheck,
  UserCheck,
  Users,
  AlertCircle,
  X,
  Search,
  Trash2,
  Settings,
} from 'lucide-react';
import {
  Button,
  Chip,
  Input,
  Label,
  Select,
  Switch,
  Textarea,
} from '@/components/ui';
import { Modal } from '@/components/Modal';
import { useI18n } from '@/i18n/locale';
import { cn } from '@/lib/cn';
import type { User } from '@/api/users';
import {
  createSchedule,
  updateSchedule,
  deleteSchedule,
  getRotationUserIds,
  type OnCallSchedule,
  type OnCallRotation,
} from '@/api/oncall';

interface ScheduleEditModalProps {
  open: boolean;
  onClose: () => void;
  mode: 'create' | 'edit';
  initialSchedule?: OnCallSchedule | null;
  users: User[];
  onSuccess: (savedScheduleId?: number) => Promise<void> | void;
  onDelete?: (scheduleId: number) => Promise<void> | void;
}

export function ScheduleEditModal({
  open,
  onClose,
  mode,
  initialSchedule,
  users,
  onSuccess,
  onDelete,
}: ScheduleEditModalProps) {
  const { tr } = useI18n();

  // Basic Information
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [timezone, setTimezone] = useState('Asia/Shanghai');
  const [handoffTime, setHandoffTime] = useState('09:00:00');
  const [reminderAdvanceHours, setReminderAdvanceHours] = useState(3);
  const [requireSwapApproval, setRequireSwapApproval] = useState(false);
  const [enabled, setEnabled] = useState(true);

  // Active Tab within Modal
  const [activeTab, setActiveTab] = useState<'basic' | 'tier1' | 'tier2'>('basic');

  // Tier 1 (Primary 一线)
  const [primaryName, setPrimaryName] = useState('一线值班轮转');
  const [primaryCadence, setPrimaryCadence] = useState<'daily' | 'weekly'>('daily');
  const [primaryUserIds, setPrimaryUserIds] = useState<number[]>([]);
  const [primarySearch, setPrimarySearch] = useState('');

  // Tier 2 (Secondary 二线)
  const [enableSecondary, setEnableSecondary] = useState(false);
  const [secondaryName, setSecondaryName] = useState('二线技术专家支持梯队');
  const [secondaryCadence, setSecondaryCadence] = useState<'daily' | 'weekly'>('weekly');
  const [secondaryUserIds, setSecondaryUserIds] = useState<number[]>([]);
  const [secondarySearch, setSecondarySearch] = useState('');
  const [secondaryTimeRestriction, setSecondaryTimeRestriction] = useState<'none' | 'time_of_day' | 'weekday'>('none');
  const [secondaryStartTime, setSecondaryStartTime] = useState('09:00:00');
  const [secondaryEndTime, setSecondaryEndTime] = useState('21:00:00');

  // Submitting state
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);

  // Reset or populate state on open
  useEffect(() => {
    if (!open) return;
    setError(null);
    setConfirmDelete(false);
    setActiveTab('basic');

    if (mode === 'edit' && initialSchedule) {
      setName(initialSchedule.name || '');
      setDescription(initialSchedule.description || '');
      setTimezone(initialSchedule.timezone || 'Asia/Shanghai');
      setHandoffTime(initialSchedule.handoff_time || '09:00:00');
      setReminderAdvanceHours(initialSchedule.reminder_advance_hours ?? 3);
      setRequireSwapApproval(initialSchedule.require_swap_approval ?? false);
      setEnabled(initialSchedule.enabled ?? true);

      // Find rotations
      const t1 = initialSchedule.rotations?.find((r) => r.tier === 1);
      if (t1) {
        setPrimaryName(t1.name || '一线值班轮转');
        setPrimaryCadence((t1.rotation_type as 'daily' | 'weekly') || 'daily');
        setPrimaryUserIds(getRotationUserIds(t1));
      } else {
        setPrimaryName('一线值班轮转');
        setPrimaryCadence('daily');
        setPrimaryUserIds([]);
      }

      const t2 = initialSchedule.rotations?.find((r) => r.tier === 2);
      if (t2) {
        setEnableSecondary(true);
        setSecondaryName(t2.name || '二线技术专家支持梯队');
        setSecondaryCadence((t2.rotation_type as 'daily' | 'weekly') || 'weekly');
        setSecondaryUserIds(getRotationUserIds(t2));
        setSecondaryTimeRestriction(t2.time_restriction_type || 'none');
        setSecondaryStartTime(t2.restriction_start_time || '09:00:00');
        setSecondaryEndTime(t2.restriction_end_time || '21:00:00');
      } else {
        setEnableSecondary(false);
        setSecondaryName('二线技术专家支持梯队');
        setSecondaryCadence('weekly');
        setSecondaryUserIds([]);
        setSecondaryTimeRestriction('none');
        setSecondaryStartTime('09:00:00');
        setSecondaryEndTime('21:00:00');
      }
    } else {
      // create mode
      setName('');
      setDescription('');
      setTimezone('Asia/Shanghai');
      setHandoffTime('09:00:00');
      setReminderAdvanceHours(3);
      setRequireSwapApproval(false);
      setEnabled(true);

      setPrimaryName('一线值班轮转');
      setPrimaryCadence('daily');
      setPrimaryUserIds([]);

      setEnableSecondary(false);
      setSecondaryName('二线技术专家支持梯队');
      setSecondaryCadence('weekly');
      setSecondaryUserIds([]);
      setSecondaryTimeRestriction('none');
      setSecondaryStartTime('09:00:00');
      setSecondaryEndTime('21:00:00');
    }
  }, [open, mode, initialSchedule]);

  // User search filters
  const filteredPrimaryUsers = useMemo(() => {
    if (!primarySearch.trim()) return users;
    const q = primarySearch.toLowerCase();
    return users.filter(
      (u) =>
        (u.display_name && u.display_name.toLowerCase().includes(q)) ||
        (u.email && u.email.toLowerCase().includes(q))
    );
  }, [users, primarySearch]);

  const filteredSecondaryUsers = useMemo(() => {
    if (!secondarySearch.trim()) return users;
    const q = secondarySearch.toLowerCase();
    return users.filter(
      (u) =>
        (u.display_name && u.display_name.toLowerCase().includes(q)) ||
        (u.email && u.email.toLowerCase().includes(q))
    );
  }, [users, secondarySearch]);

  const handleTogglePrimaryUser = (userId: number) => {
    if (primaryUserIds.includes(userId)) {
      setPrimaryUserIds(primaryUserIds.filter((id) => id !== userId));
    } else {
      setPrimaryUserIds([...primaryUserIds, userId]);
    }
  };

  const handleToggleSecondaryUser = (userId: number) => {
    if (secondaryUserIds.includes(userId)) {
      setSecondaryUserIds(secondaryUserIds.filter((id) => id !== userId));
    } else {
      setSecondaryUserIds([...secondaryUserIds, userId]);
    }
  };

  const handleSave = async () => {
    if (!name.trim()) {
      setError(tr('排班计划名称不能为空', 'Schedule name is required'));
      setActiveTab('basic');
      return;
    }
    if (primaryUserIds.length === 0) {
      setError(tr('一线主值班轮转池至少需要添加 1 位工程师', 'At least 1 engineer is required for Tier 1 primary rotation'));
      setActiveTab('tier1');
      return;
    }
    if (enableSecondary && secondaryUserIds.length === 0) {
      setError(tr('已开启二线专家支持，但未选择二线工程师', 'Secondary support is enabled but no specialists selected'));
      setActiveTab('tier2');
      return;
    }

    setSaving(true);
    setError(null);

    try {
      const primaryShiftLen = primaryCadence === 'daily' ? 86400 : 7 * 86400;
      const rotations: OnCallRotation[] = [
        {
          name: primaryName.trim() || '一线值班轮转',
          tier: 1,
          rotation_type: primaryCadence,
          shift_length_seconds: primaryShiftLen,
          users: primaryUserIds,
          effective_from: new Date().toISOString(),
          time_restriction_type: 'none',
        },
      ];

      if (enableSecondary && secondaryUserIds.length > 0) {
        const secondaryShiftLen = secondaryCadence === 'daily' ? 86400 : 7 * 86400;
        rotations.push({
          name: secondaryName.trim() || '二线技术专家支持梯队',
          tier: 2,
          rotation_type: secondaryCadence,
          shift_length_seconds: secondaryShiftLen,
          users: secondaryUserIds,
          effective_from: new Date().toISOString(),
          time_restriction_type: secondaryTimeRestriction,
          restriction_start_time:
            secondaryTimeRestriction === 'time_of_day' ? secondaryStartTime : '',
          restriction_end_time:
            secondaryTimeRestriction === 'time_of_day' ? secondaryEndTime : '',
        });
      }

      if (mode === 'create') {
        const res = await createSchedule({
          name: name.trim(),
          description: description.trim(),
          timezone: timezone.trim() || 'Asia/Shanghai',
          handoff_time: handoffTime.trim() || '09:00:00',
          reminder_advance_hours: Number(reminderAdvanceHours) || 3,
          require_swap_approval: requireSwapApproval,
          enabled,
          rotations,
        });
        await onSuccess(res.id);
      } else if (mode === 'edit' && initialSchedule) {
        const res = await updateSchedule(initialSchedule.id, {
          name: name.trim(),
          description: description.trim(),
          timezone: timezone.trim() || 'Asia/Shanghai',
          handoff_time: handoffTime.trim() || '09:00:00',
          reminder_advance_hours: Number(reminderAdvanceHours) || 3,
          require_swap_approval: requireSwapApproval,
          enabled,
          rotations,
        });
        await onSuccess(res.id);
      }
      onClose();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      setError(msg || tr('保存排班计划失败', 'Failed to save schedule'));
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    if (!initialSchedule) return;
    setDeleting(true);
    try {
      await deleteSchedule(initialSchedule.id);
      if (onDelete) {
        await onDelete(initialSchedule.id);
      }
      onClose();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      setError(msg || tr('删除排班计划失败', 'Failed to delete schedule'));
    } finally {
      setDeleting(false);
    }
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={
        mode === 'create'
          ? tr('新建值班排班计划', 'Create On-Call Schedule')
          : tr('编辑值班排班计划', 'Edit On-Call Schedule')
      }
      size="xl"
      footer={
        <div className="flex items-center justify-between w-full">
          <div>
            {mode === 'edit' && initialSchedule && (
              <div>
                {!confirmDelete ? (
                  <Button
                    size="sm"
                    variant="dangerGhost"
                    onClick={() => setConfirmDelete(true)}
                    type="button"
                  >
                    <Trash2 size={13} className="mr-1" />
                    {tr('删除排班', 'Delete Schedule')}
                  </Button>
                ) : (
                  <div className="flex items-center gap-2">
                    <span className="text-xs text-red-600 font-medium">
                      {tr('确认删除该排班吗？', 'Confirm delete?')}
                    </span>
                    <Button
                      size="sm"
                      variant="danger"
                      onClick={handleDelete}
                      disabled={deleting}
                      type="button"
                    >
                      {deleting ? tr('删除中...', 'Deleting...') : tr('确认', 'Yes')}
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => setConfirmDelete(false)}
                      type="button"
                    >
                      {tr('取消', 'No')}
                    </Button>
                  </div>
                )}
              </div>
            )}
          </div>
          <div className="flex items-center gap-2">
            <Button size="sm" variant="outline" onClick={onClose} disabled={saving || deleting}>
              {tr('取消', 'Cancel')}
            </Button>
            <Button size="sm" variant="primary" onClick={handleSave} disabled={saving || deleting}>
              {saving ? tr('保存中...', 'Saving...') : tr('保存计划', 'Save Schedule')}
            </Button>
          </div>
        </div>
      }
    >
      <div className="space-y-4 py-1">
        {error && (
          <div className="flex items-center gap-2 rounded-lg border border-red-500/30 bg-red-50 dark:bg-red-950/20 px-3.5 py-2.5 text-xs text-red-600 dark:text-red-400">
            <AlertCircle size={14} className="shrink-0" />
            <span>{error}</span>
          </div>
        )}

        {/* Navigation Tabs */}
        <div className="flex items-center gap-1 border-b border-border pb-2">
          <Button
            size="sm"
            variant={activeTab === 'basic' ? 'primary' : 'ghost'}
            onClick={() => setActiveTab('basic')}
            className="text-xs"
          >
            {tr('1. 基础配置与策略', '1. Basic Settings')}
          </Button>
          <Button
            size="sm"
            variant={activeTab === 'tier1' ? 'primary' : 'ghost'}
            onClick={() => setActiveTab('tier1')}
            className="text-xs relative"
          >
            {tr('2. 一线主值班轮转', '2. Tier 1 (Primary)')}
            <span className="ml-1 rounded-full bg-indigo-500/20 px-1.5 py-0.2 text-[10px] font-bold">
              {primaryUserIds.length}
            </span>
          </Button>
          <Button
            size="sm"
            variant={activeTab === 'tier2' ? 'primary' : 'ghost'}
            onClick={() => setActiveTab('tier2')}
            className="text-xs relative"
          >
            {tr('3. 二线专家支持梯队', '3. Tier 2 (Secondary)')}
            {enableSecondary && (
              <span className="ml-1 rounded-full bg-sky-500/20 px-1.5 py-0.2 text-[10px] font-bold text-sky-600">
                {secondaryUserIds.length}
              </span>
            )}
          </Button>
        </div>

        {/* Tab 1: Basic Information */}
        {activeTab === 'basic' && (
          <div className="space-y-4 animate-fade-in">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <Label className="text-xs mb-1 block">{tr('排班计划名称 *', 'Schedule Name *')}</Label>
                <Input
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="e.g. SRE 核心生产运维 7x24h 值班"
                  className="w-full text-xs"
                />
              </div>

              <div>
                <Label className="text-xs mb-1 block">{tr('时区 (Timezone)', 'Timezone')}</Label>
                <Select
                  value={timezone}
                  onValueChange={(val) => setTimezone(val)}
                  options={[
                    { value: 'Asia/Shanghai', label: 'Asia/Shanghai (UTC+8)' },
                    { value: 'UTC', label: 'UTC (GMT+0)' },
                    { value: 'America/New_York', label: 'America/New_York (EST)' },
                    { value: 'Europe/London', label: 'Europe/London (GMT)' },
                    { value: 'Asia/Tokyo', label: 'Asia/Tokyo (JST)' },
                  ]}
                  className="w-full text-xs"
                />
              </div>
            </div>

            <div>
              <Label className="text-xs mb-1 block">{tr('描述', 'Description')}</Label>
              <Textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="简述该排班对应的业务线、服务范围与响应要求"
                rows={2}
                className="w-full text-xs"
              />
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <Label className="text-xs mb-1 block">
                  {tr('每日交接时刻点 (Handoff Time)', 'Handoff Time (HH:MM:SS)')}
                </Label>
                <Input
                  value={handoffTime}
                  onChange={(e) => setHandoffTime(e.target.value)}
                  placeholder="09:00:00"
                  className="w-full text-xs"
                />
                <span className="text-[11px] text-text-muted mt-1 block">
                  {tr('轮转班次每日在该时刻进行自动平滑交接', 'Shift transitions automatically at this daily hour.')}
                </span>
              </div>

              <div>
                <Label className="text-xs mb-1 block">
                  {tr('提前提醒时间 (小时)', 'Reminder Advance Hours')}
                </Label>
                <Input
                  type="number"
                  min={1}
                  max={48}
                  value={reminderAdvanceHours}
                  onChange={(e) => setReminderAdvanceHours(Number(e.target.value))}
                  className="w-full text-xs"
                />
                <span className="text-[11px] text-text-muted mt-1 block">
                  {tr('在接班前多久向接班人发送交接班提醒通知', 'Advance hours before notifying upcoming on-call assignee.')}
                </span>
              </div>
            </div>

            <div className="rounded-lg border border-border p-3.5 bg-bg/40 space-y-3">
              <div className="flex items-center justify-between">
                <div>
                  <div className="text-xs font-semibold text-text">
                    {tr('换班/代班是否需要审批', 'Require Swap Approval')}
                  </div>
                  <div className="text-[11px] text-text-muted">
                    {tr(
                      '开启后，工程师申请换班需由原值班人或主管审批后方才生效；关闭则即刻生效',
                      'Require confirmation from original assignee/lead before swap takes effect.'
                    )}
                  </div>
                </div>
                <Switch
                  checked={requireSwapApproval}
                  onCheckedChange={setRequireSwapApproval}
                />
              </div>

              <div className="border-t border-border pt-3 flex items-center justify-between">
                <div>
                  <div className="text-xs font-semibold text-text">
                    {tr('启用该排班计划', 'Enable Schedule')}
                  </div>
                  <div className="text-[11px] text-text-muted">
                    {tr('停用后不再生成值班日历班次与告警路由', 'When disabled, shifts and route assignments pause.')}
                  </div>
                </div>
                <Switch
                  checked={enabled}
                  onCheckedChange={setEnabled}
                />
              </div>
            </div>
          </div>
        )}

        {/* Tab 2: Tier 1 Primary Rotation */}
        {activeTab === 'tier1' && (
          <div className="space-y-4 animate-fade-in">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <Label className="text-xs mb-1 block">{tr('一线轮转名称', 'Primary Rotation Name')}</Label>
                <Input
                  value={primaryName}
                  onChange={(e) => setPrimaryName(e.target.value)}
                  placeholder="一线主值班轮转"
                  className="w-full text-xs"
                />
              </div>
              <div>
                <Label className="text-xs mb-1 block">{tr('轮转周期', 'Cadence')}</Label>
                <div className="flex items-center gap-2">
                  <Button
                    size="sm"
                    variant={primaryCadence === 'daily' ? 'primary' : 'outline'}
                    onClick={() => setPrimaryCadence('daily')}
                    className="flex-1"
                    type="button"
                  >
                    {tr('按天 (Daily)', 'Daily')}
                  </Button>
                  <Button
                    size="sm"
                    variant={primaryCadence === 'weekly' ? 'primary' : 'outline'}
                    onClick={() => setPrimaryCadence('weekly')}
                    className="flex-1"
                    type="button"
                  >
                    {tr('按周 (Weekly)', 'Weekly')}
                  </Button>
                </div>
              </div>
            </div>

            {/* Selected Users */}
            <div>
              <div className="flex items-center justify-between mb-1.5">
                <Label className="text-xs font-semibold text-text flex items-center gap-1.5">
                  <UserCheck size={14} className="text-indigo-500" />
                  <span>{tr('一线值班人员轮转池', 'Primary Engineer Sequence')}</span>
                  <span className="text-text-muted font-normal">
                    ({primaryUserIds.length} {tr('人', 'users')})
                  </span>
                </Label>
                {primaryUserIds.length > 0 && (
                  <button
                    type="button"
                    onClick={() => setPrimaryUserIds([])}
                    className="text-[11px] text-text-muted hover:text-red-500"
                  >
                    {tr('清空', 'Clear')}
                  </button>
                )}
              </div>

              {primaryUserIds.length === 0 ? (
                <div className="rounded-lg border border-dashed border-border p-3 text-center text-xs text-text-muted">
                  {tr('暂未选择一线值班人员，请在下方列表中点击添加。', 'No primary engineers selected.')}
                </div>
              ) : (
                <div className="flex flex-wrap gap-1.5 p-2 rounded-lg border border-border bg-bg/40 max-h-32 overflow-y-auto">
                  {primaryUserIds.map((userId, idx) => {
                    const u = users.find((user) => user.id === userId);
                    return (
                      <div
                        key={userId}
                        className="flex items-center gap-1.5 rounded-md border border-indigo-500/30 bg-indigo-50 dark:bg-indigo-950/40 px-2 py-1 text-xs"
                      >
                        <span className="inline-flex h-4 w-4 items-center justify-center rounded-full bg-indigo-600 text-[10px] font-bold text-white">
                          {idx + 1}
                        </span>
                        <span className="font-medium text-text">
                          {u?.display_name || u?.email || `User #${userId}`}
                        </span>
                        <button
                          type="button"
                          onClick={() => setPrimaryUserIds(primaryUserIds.filter((id) => id !== userId))}
                          className="text-text-faint hover:text-red-500"
                        >
                          <X size={12} />
                        </button>
                      </div>
                    );
                  })}
                </div>
              )}
            </div>

            {/* Member Selection List */}
            <div>
              <div className="flex items-center justify-between mb-1.5">
                <Label className="text-xs text-text-muted">{tr('从系统成员中选择：', 'Select from team:')}</Label>
                <div className="relative w-44">
                  <Search size={12} className="absolute left-2.5 top-2.5 text-text-muted" />
                  <Input
                    type="search"
                    value={primarySearch}
                    onChange={(e) => setPrimarySearch(e.target.value)}
                    placeholder={tr('搜索成员...', 'Search users...')}
                    className="h-7 text-xs pl-7"
                  />
                </div>
              </div>

              <div className="max-h-44 overflow-y-auto rounded-lg border border-border divide-y divide-border bg-card">
                {filteredPrimaryUsers.map((u) => {
                  const isSelected = primaryUserIds.includes(u.id);
                  const order = primaryUserIds.indexOf(u.id);
                  return (
                    <div
                      key={u.id}
                      onClick={() => handleTogglePrimaryUser(u.id)}
                      className={cn(
                        'flex items-center justify-between px-3 py-1.5 cursor-pointer text-xs transition-colors',
                        isSelected ? 'bg-indigo-50/50 dark:bg-indigo-950/30' : 'hover:bg-bg'
                      )}
                    >
                      <div className="flex items-center gap-2">
                        <div
                          className={cn(
                            'flex h-5 w-5 items-center justify-center rounded-full text-[10px] font-bold',
                            isSelected ? 'bg-indigo-600 text-white' : 'bg-zinc-100 dark:bg-zinc-800 text-text-muted'
                          )}
                        >
                          {isSelected ? order + 1 : u.display_name?.[0] || 'U'}
                        </div>
                        <span className="font-medium text-text">{u.display_name || u.email}</span>
                        <span className="text-[11px] text-text-muted">{u.email}</span>
                      </div>
                      <Chip tone={isSelected ? 'accent' : 'default'} dense>
                        {isSelected ? tr('已选', 'Selected') : tr('添加', 'Add')}
                      </Chip>
                    </div>
                  );
                })}
              </div>
            </div>
          </div>
        )}

        {/* Tab 3: Tier 2 Secondary Rotation */}
        {activeTab === 'tier2' && (
          <div className="space-y-4 animate-fade-in">
            <div className="rounded-lg border border-border p-3.5 bg-bg/40 flex items-center justify-between">
              <div>
                <div className="text-xs font-semibold text-text flex items-center gap-2">
                  <ShieldCheck size={16} className="text-sky-500" />
                  <span>{tr('启用二线技术专家支持梯队', 'Enable Secondary Specialist Tier')}</span>
                </div>
                <div className="text-[11px] text-text-muted mt-0.5">
                  {tr(
                    '一线未在规定时限内响应或遇疑难突发重大事故时，系统将升级通知此专家队列',
                    'Escalate to this specialist pool when primary fails to ack or incident escalates.'
                  )}
                </div>
              </div>
              <Switch
                checked={enableSecondary}
                onCheckedChange={setEnableSecondary}
              />
            </div>

            {enableSecondary ? (
              <div className="space-y-4 pt-1">
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  <div>
                    <Label className="text-xs mb-1 block">{tr('二线轮转名称', 'Secondary Rotation Name')}</Label>
                    <Input
                      value={secondaryName}
                      onChange={(e) => setSecondaryName(e.target.value)}
                      placeholder="二线技术专家支持梯队"
                      className="w-full text-xs"
                    />
                  </div>
                  <div>
                    <Label className="text-xs mb-1 block">{tr('轮转周期', 'Cadence')}</Label>
                    <div className="flex items-center gap-2">
                      <Button
                        size="sm"
                        variant={secondaryCadence === 'daily' ? 'primary' : 'outline'}
                        onClick={() => setSecondaryCadence('daily')}
                        className="flex-1"
                        type="button"
                      >
                        {tr('按天 (Daily)', 'Daily')}
                      </Button>
                      <Button
                        size="sm"
                        variant={secondaryCadence === 'weekly' ? 'primary' : 'outline'}
                        onClick={() => setSecondaryCadence('weekly')}
                        className="flex-1"
                        type="button"
                      >
                        {tr('按周 (Weekly)', 'Weekly')}
                      </Button>
                    </div>
                  </div>
                </div>

                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  <div>
                    <Label className="text-xs mb-1 block">{tr('时段限制', 'Time Restriction')}</Label>
                    <Select
                      value={secondaryTimeRestriction}
                      onValueChange={(val) => setSecondaryTimeRestriction(val as 'none' | 'time_of_day' | 'weekday')}
                      options={[
                        { value: 'none', label: tr('全天 24 小时随行待命', '24/7 Always On-Call') },
                        { value: 'weekday', label: tr('仅工作日待命 (周一至周五)', 'Weekdays Only') },
                        { value: 'time_of_day', label: tr('限定时间窗口 (如日间)', 'Time Window') },
                      ]}
                      className="w-full text-xs"
                    />
                  </div>
                  {secondaryTimeRestriction === 'time_of_day' && (
                    <div className="flex items-center gap-2">
                      <div className="flex-1">
                        <Label className="text-xs mb-1 block">{tr('开始', 'Start')}</Label>
                        <Input
                          value={secondaryStartTime}
                          onChange={(e) => setSecondaryStartTime(e.target.value)}
                          className="w-full text-xs"
                        />
                      </div>
                      <div className="flex-1">
                        <Label className="text-xs mb-1 block">{tr('结束', 'End')}</Label>
                        <Input
                          value={secondaryEndTime}
                          onChange={(e) => setSecondaryEndTime(e.target.value)}
                          className="w-full text-xs"
                        />
                      </div>
                    </div>
                  )}
                </div>

                {/* Selected Secondary Users */}
                <div>
                  <div className="flex items-center justify-between mb-1.5">
                    <Label className="text-xs font-semibold text-text flex items-center gap-1.5">
                      <Users size={14} className="text-sky-500" />
                      <span>{tr('二线专家轮转池', 'Secondary Specialist Sequence')}</span>
                      <span className="text-text-muted font-normal">
                        ({secondaryUserIds.length} {tr('人', 'users')})
                      </span>
                    </Label>
                    {secondaryUserIds.length > 0 && (
                      <button
                        type="button"
                        onClick={() => setSecondaryUserIds([])}
                        className="text-[11px] text-text-muted hover:text-red-500"
                      >
                        {tr('清空', 'Clear')}
                      </button>
                    )}
                  </div>

                  {secondaryUserIds.length === 0 ? (
                    <div className="rounded-lg border border-dashed border-border p-3 text-center text-xs text-text-muted">
                      {tr('暂未选择二线专家，请在下方列表中勾选。', 'No specialists selected.')}
                    </div>
                  ) : (
                    <div className="flex flex-wrap gap-1.5 p-2 rounded-lg border border-border bg-bg/40 max-h-32 overflow-y-auto">
                      {secondaryUserIds.map((userId, idx) => {
                        const u = users.find((user) => user.id === userId);
                        return (
                          <div
                            key={userId}
                            className="flex items-center gap-1.5 rounded-md border border-sky-500/30 bg-sky-50 dark:bg-sky-950/40 px-2 py-1 text-xs"
                          >
                            <span className="inline-flex h-4 w-4 items-center justify-center rounded-full bg-sky-500 text-[10px] font-bold text-white">
                              {idx + 1}
                            </span>
                            <span className="font-medium text-text">
                              {u?.display_name || u?.email || `User #${userId}`}
                            </span>
                            <button
                              type="button"
                              onClick={() => setSecondaryUserIds(secondaryUserIds.filter((id) => id !== userId))}
                              className="text-text-faint hover:text-red-500"
                            >
                              <X size={12} />
                            </button>
                          </div>
                        );
                      })}
                    </div>
                  )}
                </div>

                {/* Secondary Member Checklist */}
                <div>
                  <div className="flex items-center justify-between mb-1.5">
                    <Label className="text-xs text-text-muted">{tr('选择专家成员：', 'Select specialists:')}</Label>
                    <div className="relative w-44">
                      <Search size={12} className="absolute left-2.5 top-2.5 text-text-muted" />
                      <Input
                        type="search"
                        value={secondarySearch}
                        onChange={(e) => setSecondarySearch(e.target.value)}
                        placeholder={tr('搜索成员...', 'Search users...')}
                        className="h-7 text-xs pl-7"
                      />
                    </div>
                  </div>

                  <div className="max-h-44 overflow-y-auto rounded-lg border border-border divide-y divide-border bg-card">
                    {filteredSecondaryUsers.map((u) => {
                      const isSelected = secondaryUserIds.includes(u.id);
                      const order = secondaryUserIds.indexOf(u.id);
                      return (
                        <div
                          key={u.id}
                          onClick={() => handleToggleSecondaryUser(u.id)}
                          className={cn(
                            'flex items-center justify-between px-3 py-1.5 cursor-pointer text-xs transition-colors',
                            isSelected ? 'bg-sky-50/50 dark:bg-sky-950/30' : 'hover:bg-bg'
                          )}
                        >
                          <div className="flex items-center gap-2">
                            <div
                              className={cn(
                                'flex h-5 w-5 items-center justify-center rounded-full text-[10px] font-bold',
                                isSelected ? 'bg-sky-500 text-white' : 'bg-zinc-100 dark:bg-zinc-800 text-text-muted'
                              )}
                            >
                              {isSelected ? order + 1 : u.display_name?.[0] || 'U'}
                            </div>
                            <span className="font-medium text-text">{u.display_name || u.email}</span>
                            <span className="text-[11px] text-text-muted">{u.email}</span>
                          </div>
                          <Chip tone={isSelected ? 'accent' : 'default'} dense>
                            {isSelected ? tr('已选', 'Selected') : tr('添加', 'Add')}
                          </Chip>
                        </div>
                      );
                    })}
                  </div>
                </div>
              </div>
            ) : (
              <div className="p-6 text-center text-xs text-text-muted rounded-lg border border-dashed border-border">
                {tr(
                  '当前排班未启用二线专家支持。如需配置研发二线升级响应，请打开上方开关。',
                  'Secondary support disabled. Turn on the switch above to configure.'
                )}
              </div>
            )}
          </div>
        )}
      </div>
    </Modal>
  );
}
