import { useState, useEffect, useMemo } from 'react';
import {
  Users,
  Clock,
  Calendar,
  AlertCircle,
  X,
  Search,
  ShieldCheck,
} from 'lucide-react';
import {
  Button,
  Chip,
  Input,
  Label,
  Select,
} from '@/components/ui';
import { Modal } from '@/components/Modal';
import { useI18n } from '@/i18n/locale';
import { cn } from '@/lib/cn';
import type { User } from '@/api/users';
import {
  setSecondaryRotation,
  getRotationUserIds,
  type OnCallSchedule,
  type OnCallRotation,
  type SetSecondaryRotationInput,
} from '@/api/oncall';

interface SecondaryRotationModalProps {
  open: boolean;
  onClose: () => void;
  schedule: OnCallSchedule | null;
  existingRotation?: OnCallRotation | null;
  users: User[];
  onSuccess: () => Promise<void> | void;
}

export function SecondaryRotationModal({
  open,
  onClose,
  schedule,
  existingRotation,
  users,
  onSuccess,
}: SecondaryRotationModalProps) {
  const { tr } = useI18n();

  const [name, setName] = useState('');
  const [cadence, setCadence] = useState<'daily' | 'weekly' | 'custom'>('daily');
  const [customDays, setCustomDays] = useState(3);
  const [selectedUserIds, setSelectedUserIds] = useState<number[]>([]);
  const [userSearch, setUserSearch] = useState('');
  const [effectiveFrom, setEffectiveFrom] = useState('');
  const [timeRestrictionType, setTimeRestrictionType] = useState<'none' | 'time_of_day' | 'weekday'>('none');
  const [restrictionStartTime, setRestrictionStartTime] = useState('09:00:00');
  const [restrictionEndTime, setRestrictionEndTime] = useState('21:00:00');

  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Initialize form when opening or when existingRotation changes
  useEffect(() => {
    if (open) {
      setError(null);
      if (existingRotation) {
        setName(existingRotation.name || tr('二线技术专家支持梯队', 'Secondary Escalation Tier'));
        setCadence(existingRotation.rotation_type || 'daily');
        if (existingRotation.rotation_type === 'custom') {
          const days = Math.round((existingRotation.shift_length_seconds || 86400) / 86400);
          setCustomDays(days > 0 ? days : 1);
        }
        setSelectedUserIds(getRotationUserIds(existingRotation));
        if (existingRotation.effective_from) {
          try {
            setEffectiveFrom(existingRotation.effective_from.slice(0, 10));
          } catch {
            setEffectiveFrom(new Date().toISOString().slice(0, 10));
          }
        } else {
          setEffectiveFrom(new Date().toISOString().slice(0, 10));
        }
        setTimeRestrictionType(existingRotation.time_restriction_type || 'none');
        setRestrictionStartTime(existingRotation.restriction_start_time || '09:00:00');
        setRestrictionEndTime(existingRotation.restriction_end_time || '21:00:00');
      } else {
        setName(tr('二线技术专家支持梯队', 'Secondary Escalation Tier'));
        setCadence('weekly');
        setSelectedUserIds([]);
        setEffectiveFrom(new Date().toISOString().slice(0, 10));
        setTimeRestrictionType('none');
        setRestrictionStartTime('09:00:00');
        setRestrictionEndTime('21:00:00');
      }
    }
  }, [open, existingRotation, tr]);

  const filteredUsers = useMemo(() => {
    if (!userSearch.trim()) return users;
    const q = userSearch.toLowerCase();
    return users.filter(
      (u) =>
        (u.display_name && u.display_name.toLowerCase().includes(q)) ||
        (u.email && u.email.toLowerCase().includes(q)) ||
        (u.phone && u.phone.includes(q))
    );
  }, [users, userSearch]);

  const handleToggleUser = (userId: number) => {
    if (selectedUserIds.includes(userId)) {
      setSelectedUserIds(selectedUserIds.filter((id) => id !== userId));
    } else {
      setSelectedUserIds([...selectedUserIds, userId]);
    }
  };

  const handleRemoveUser = (userId: number) => {
    setSelectedUserIds(selectedUserIds.filter((id) => id !== userId));
  };

  const handleMoveUser = (idx: number, direction: 'up' | 'down') => {
    const targetIdx = direction === 'up' ? idx - 1 : idx + 1;
    if (targetIdx < 0 || targetIdx >= selectedUserIds.length) return;
    const next = [...selectedUserIds];
    const temp = next[idx];
    next[idx] = next[targetIdx];
    next[targetIdx] = temp;
    setSelectedUserIds(next);
  };

  const handleSubmit = async () => {
    if (!schedule) return;
    if (!name.trim()) {
      setError(tr('请输入二线轮转名称', 'Please enter a rotation name'));
      return;
    }
    if (selectedUserIds.length === 0) {
      setError(tr('请至少选择一位二线专家工程师', 'Please select at least one specialist user'));
      return;
    }

    setSaving(true);
    setError(null);
    try {
      let shiftLen = 86400;
      if (cadence === 'weekly') {
        shiftLen = 7 * 86400;
      } else if (cadence === 'custom') {
        shiftLen = Math.max(1, customDays) * 86400;
      }

      const eff = effectiveFrom
        ? new Date(effectiveFrom + 'T00:00:00Z').toISOString()
        : new Date().toISOString();

      const payload: SetSecondaryRotationInput = {
        name: name.trim(),
        rotation_type: cadence,
        shift_length_seconds: shiftLen,
        users: selectedUserIds,
        effective_from: eff,
        time_restriction_type: timeRestrictionType,
        restriction_start_time:
          timeRestrictionType === 'time_of_day' ? restrictionStartTime : '',
        restriction_end_time:
          timeRestrictionType === 'time_of_day' ? restrictionEndTime : '',
      };

      await setSecondaryRotation(schedule.id, payload);
      await onSuccess();
      onClose();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      setError(msg || tr('保存二线专家配置失败', 'Failed to save secondary rotation'));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={
        existingRotation
          ? tr('配置二线技术专家支持梯队', 'Configure Secondary Specialist Rotation')
          : tr('新增二线技术专家支持梯队', 'Add Secondary Specialist Rotation')
      }
      size="lg"
      footer={
        <div className="flex items-center justify-between w-full">
          <div className="text-xs text-text-muted">
            {schedule && (
              <span>
                {tr('所属排班：', 'Schedule: ')}
                <span className="font-medium text-text">{schedule.name}</span>
              </span>
            )}
          </div>
          <div className="flex items-center gap-2">
            <Button size="sm" variant="outline" onClick={onClose} disabled={saving}>
              {tr('取消', 'Cancel')}
            </Button>
            <Button
              size="sm"
              variant="primary"
              onClick={handleSubmit}
              disabled={saving || selectedUserIds.length === 0}
            >
              {saving ? tr('保存中...', 'Saving...') : tr('确认保存', 'Save Rotation')}
            </Button>
          </div>
        </div>
      }
    >
      <div className="space-y-5 py-1">
        {error && (
          <div className="flex items-center gap-2 rounded-lg border border-red-500/30 bg-red-50 dark:bg-red-950/20 px-3.5 py-2.5 text-xs text-red-600 dark:text-red-400">
            <AlertCircle size={14} className="shrink-0" />
            <span>{error}</span>
          </div>
        )}

        <div className="rounded-lg border border-sky-500/20 bg-sky-50/40 dark:bg-sky-950/20 p-3 text-xs text-sky-800 dark:text-sky-300">
          <div className="font-semibold mb-0.5">
            {tr('💡 什么是二线专家梯队 (Secondary Escalation Tier)？', '💡 What is Secondary Escalation Tier?')}
          </div>
          <div className="text-text-muted dark:text-sky-200/80 leading-relaxed">
            {tr(
              '一线主值班人超时未认领、或处置重大故障需专项专家升级协助时，系统将依据告警路由自动呼叫或升级至当前周期的二线专家支持工程师。',
              'When primary on-call times out or incidents require expert escalation, OpsPilot routes notifications directly to the scheduled secondary specialists.'
            )}
          </div>
        </div>

        {/* Basic settings */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div>
            <Label className="text-xs mb-1 block">
              {tr('二线轮转名称 *', 'Rotation Name *')}
            </Label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. 二线技术专家支持梯队"
              className="w-full text-xs"
            />
          </div>

          <div>
            <Label className="text-xs mb-1 block">
              {tr('轮转周期 *', 'Rotation Cadence *')}
            </Label>
            <div className="flex items-center gap-2">
              <Button
                size="sm"
                variant={cadence === 'daily' ? 'primary' : 'outline'}
                onClick={() => setCadence('daily')}
                className="flex-1"
                type="button"
              >
                {tr('按天 (Daily)', 'Daily')}
              </Button>
              <Button
                size="sm"
                variant={cadence === 'weekly' ? 'primary' : 'outline'}
                onClick={() => setCadence('weekly')}
                className="flex-1"
                type="button"
              >
                {tr('按周 (Weekly)', 'Weekly')}
              </Button>
              <Button
                size="sm"
                variant={cadence === 'custom' ? 'primary' : 'outline'}
                onClick={() => setCadence('custom')}
                className="flex-1"
                type="button"
              >
                {tr('自定义', 'Custom')}
              </Button>
            </div>
            {cadence === 'custom' && (
              <div className="mt-2 flex items-center gap-2">
                <span className="text-xs text-text-muted">{tr('每班时长：', 'Shift length: ')}</span>
                <Input
                  type="number"
                  min={1}
                  max={30}
                  value={customDays}
                  onChange={(e) => setCustomDays(Number(e.target.value))}
                  className="w-20 text-xs text-center"
                />
                <span className="text-xs text-text-muted">{tr('天', 'days')}</span>
              </div>
            )}
          </div>
        </div>

        {/* Time restrictions & Effective from */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div>
            <Label className="text-xs mb-1 block">
              {tr('生效起始日期', 'Effective From Date')}
            </Label>
            <Input
              type="date"
              value={effectiveFrom}
              onChange={(e) => setEffectiveFrom(e.target.value)}
              className="w-full text-xs"
            />
          </div>

          <div>
            <Label className="text-xs mb-1 block">
              {tr('专家支持时段限制', 'Time Restriction')}
            </Label>
            <Select
              value={timeRestrictionType}
              onValueChange={(val) => setTimeRestrictionType(val as 'none' | 'time_of_day' | 'weekday')}
              options={[
                { value: 'none', label: tr('全天 24 小时随行待命', '24/7 Always On-Call') },
                { value: 'weekday', label: tr('仅工作日待命 (周一至周五)', 'Weekdays Only (Mon-Fri)') },
                { value: 'time_of_day', label: tr('限定时间窗口 (如日间支持)', 'Time Window (e.g. Daytime)') },
              ]}
              className="w-full text-xs"
            />
          </div>
        </div>

        {timeRestrictionType === 'time_of_day' && (
          <div className="grid grid-cols-2 gap-3 p-3 rounded-lg border border-border bg-bg/50">
            <div>
              <Label className="text-xs text-text-muted">{tr('每日开始时刻', 'Daily Start Time')}</Label>
              <Input
                type="text"
                value={restrictionStartTime}
                onChange={(e) => setRestrictionStartTime(e.target.value)}
                placeholder="09:00:00"
                className="mt-1 text-xs"
              />
            </div>
            <div>
              <Label className="text-xs text-text-muted">{tr('每日结束时刻', 'Daily End Time')}</Label>
              <Input
                type="text"
                value={restrictionEndTime}
                onChange={(e) => setRestrictionEndTime(e.target.value)}
                placeholder="21:00:00"
                className="mt-1 text-xs"
              />
            </div>
          </div>
        )}

        {/* Selected Specialists Pool & Ordering */}
        <div>
          <div className="flex items-center justify-between mb-1.5">
            <Label className="text-xs font-semibold text-text flex items-center gap-1.5">
              <Users size={14} className="text-sky-500" />
              <span>{tr('二线专家轮转池 (按序循环)', 'Specialist Rotation Sequence')}</span>
              <span className="text-text-muted font-normal">
                ({selectedUserIds.length} {tr('人', 'users')})
              </span>
            </Label>
            {selectedUserIds.length > 0 && (
              <button
                type="button"
                onClick={() => setSelectedUserIds([])}
                className="text-[11px] text-text-muted hover:text-red-500 transition-colors"
              >
                {tr('清空全部', 'Clear all')}
              </button>
            )}
          </div>

          {selectedUserIds.length === 0 ? (
            <div className="rounded-lg border border-dashed border-border p-4 text-center text-xs text-text-muted">
              {tr(
                '暂未添加二线专家，请在下方人员列表中点击勾选加入轮转池。',
                'No specialists selected. Click members from the list below to add.'
              )}
            </div>
          ) : (
            <div className="flex flex-wrap gap-2 p-2.5 rounded-lg border border-border bg-bg/40 max-h-40 overflow-y-auto">
              {selectedUserIds.map((userId, idx) => {
                const u = users.find((user) => user.id === userId);
                return (
                  <div
                    key={userId}
                    className="flex items-center gap-1.5 rounded-md border border-sky-500/30 bg-sky-50 dark:bg-sky-950/40 px-2 py-1 text-xs shadow-xs"
                  >
                    <span className="inline-flex h-4 w-4 items-center justify-center rounded-full bg-sky-500 text-[10px] font-bold text-white">
                      {idx + 1}
                    </span>
                    <span className="font-medium text-text">
                      {u?.display_name || u?.email || `User #${userId}`}
                    </span>
                    {idx > 0 && (
                      <button
                        type="button"
                        onClick={() => handleMoveUser(idx, 'up')}
                        title={tr('向前调整', 'Move up')}
                        className="text-text-faint hover:text-text px-0.5 text-[10px]"
                      >
                        ◀
                      </button>
                    )}
                    {idx < selectedUserIds.length - 1 && (
                      <button
                        type="button"
                        onClick={() => handleMoveUser(idx, 'down')}
                        title={tr('向后调整', 'Move down')}
                        className="text-text-faint hover:text-text px-0.5 text-[10px]"
                      >
                        ▶
                      </button>
                    )}
                    <button
                      type="button"
                      onClick={() => handleRemoveUser(userId)}
                      className="text-text-faint hover:text-red-500 ml-0.5"
                    >
                      <X size={12} />
                    </button>
                  </div>
                );
              })}
            </div>
          )}
        </div>

        {/* Member selection table / checklist */}
        <div>
          <div className="flex items-center justify-between mb-2">
            <Label className="text-xs text-text-muted">
              {tr('从团队成员中选择专家工程师 (点击添加 / 移出)：', 'Select specialist from team members:')}
            </Label>
            <div className="relative w-48">
              <Search size={12} className="absolute left-2.5 top-2.5 text-text-muted" />
              <Input
                type="search"
                value={userSearch}
                onChange={(e) => setUserSearch(e.target.value)}
                placeholder={tr('搜索工程师...', 'Search users...')}
                className="h-7 text-xs pl-7"
              />
            </div>
          </div>

          <div className="max-h-48 overflow-y-auto rounded-lg border border-border divide-y divide-border bg-card">
            {filteredUsers.length === 0 ? (
              <div className="py-6 text-center text-xs text-text-muted">
                {tr('无匹配的成员', 'No members found')}
              </div>
            ) : (
              filteredUsers.map((u) => {
                const isSelected = selectedUserIds.includes(u.id);
                const orderIdx = selectedUserIds.indexOf(u.id);

                return (
                  <div
                    key={u.id}
                    onClick={() => handleToggleUser(u.id)}
                    className={cn(
                      'flex items-center justify-between px-3 py-2 cursor-pointer text-xs transition-colors',
                      isSelected
                        ? 'bg-sky-50/60 dark:bg-sky-950/30'
                        : 'hover:bg-bg'
                    )}
                  >
                    <div className="flex items-center gap-2.5">
                      <div
                        className={cn(
                          'flex h-6 w-6 items-center justify-center rounded-full text-[11px] font-bold transition-colors',
                          isSelected
                            ? 'bg-sky-500 text-white'
                            : 'bg-zinc-100 dark:bg-zinc-800 text-text-muted'
                        )}
                      >
                        {isSelected ? orderIdx + 1 : u.display_name?.[0] || 'U'}
                      </div>
                      <div>
                        <div className="font-medium text-text flex items-center gap-1.5">
                          <span>{u.display_name || u.email}</span>
                          <Chip tone={u.role === 'admin' ? 'accent' : 'default'} dense>
                            {u.role}
                          </Chip>
                        </div>
                        <div className="text-[11px] text-text-muted">
                          {u.email}
                          {u.phone ? ` · ${u.phone}` : ''}
                        </div>
                      </div>
                    </div>

                    <div>
                      {isSelected ? (
                        <Chip tone="success" dense>
                          {tr('已选入轮转', 'Selected')}
                        </Chip>
                      ) : (
                        <Button size="sm" variant="ghost" className="h-6 text-[11px] px-2">
                          {tr('加入', 'Add')}
                        </Button>
                      )}
                    </div>
                  </div>
                );
              })
            )}
          </div>
        </div>
      </div>
    </Modal>
  );
}
