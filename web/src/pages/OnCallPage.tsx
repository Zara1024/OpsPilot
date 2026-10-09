import { useCallback, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  CalendarDays,
  ChevronLeft,
  ChevronRight,
  Clock,
  Plus,
  RefreshCw,
  UserCheck,
  AlertTriangle,
  GitFork,
  BellRing,
  Activity,
  PhoneCall,
  Settings,
  ShieldCheck,
} from 'lucide-react';
import {
  Button,
  Card,
  Chip,
  EmptyState,
  Input,
  Label,
  PageHeader,
  Select,
  Textarea,
  Tabs,
  TabsList,
  TabsTrigger,
  TabsContent,
} from '@/components/ui';
import { Modal } from '@/components/Modal';
import { useI18n } from '@/i18n/locale';
import { cn } from '@/lib/cn';
import { listUsers, type User } from '@/api/users';
import {
  createOverride,
  createSchedule,
  getCalendarShifts,
  getCurrentOnCall,
  listSchedules,
  listPendingOverrides,
  listEscalationPolicies,
  type GapSlot,
  type LiveOnCallStatus,
  type OnCallSchedule,
  type ShiftSlot,
  type EscalationPolicy,
} from '@/api/oncall';
import { RouteTreeTab } from './oncall/RouteTreeTab';
import { EscalationChatOpsTab } from './oncall/EscalationChatOpsTab';
import { SwapApprovalsTab } from './oncall/SwapApprovalsTab';
import { HandoffAnalyticsTab } from './oncall/HandoffAnalyticsTab';
import { VoiceGatewayTab } from './oncall/VoiceGatewayTab';
import { SecondaryRotationModal } from './oncall/SecondaryRotationModal';
import { ScheduleEditModal } from './oncall/ScheduleEditModal';

type ViewMode = 'month' | 'week' | 'day';

function formatTime(isoString?: string): string {
  if (!isoString) return '';
  try {
    const d = new Date(isoString);
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });
  } catch {
    return isoString.slice(11, 16);
  }
}

function toDateTimeLocalValue(isoString?: string): string {
  if (!isoString) return '';
  try {
    const d = new Date(isoString);
    const pad = (n: number) => String(n).padStart(2, '0');
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
  } catch {
    return isoString.slice(0, 16);
  }
}

export default function OnCallPage() {
  const { tr } = useI18n();
  const [searchParams, setSearchParams] = useSearchParams();
  const currentTab = searchParams.get('tab') || 'board';

  // Data states
  const [schedules, setSchedules] = useState<OnCallSchedule[]>([]);
  const [selectedScheduleId, setSelectedScheduleId] = useState<number | null>(null);
  const [liveStatus, setLiveStatus] = useState<LiveOnCallStatus | null>(null);
  const [shifts, setShifts] = useState<ShiftSlot[]>([]);
  const [gaps, setGaps] = useState<GapSlot[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [pendingCount, setPendingCount] = useState<number>(0);
  const [escalationPolicies, setEscalationPolicies] = useState<EscalationPolicy[]>([]);

  // UI state
  const [loading, setLoading] = useState(true);
  const [calendarLoading, setCalendarLoading] = useState(false);
  const [viewMode, setViewMode] = useState<ViewMode>('month');
  const [currentDate, setCurrentDate] = useState<Date>(new Date());
  const [countdown, setCountdown] = useState<string>('');

  // Modals
  const [scheduleModalOpen, setScheduleModalOpen] = useState(false);
  const [scheduleModalMode, setScheduleModalMode] = useState<'create' | 'edit'>('create');
  const [secondaryModalOpen, setSecondaryModalOpen] = useState(false);

  const [overrideModalOpen, setOverrideModalOpen] = useState(false);
  const [overrideShift, setOverrideShift] = useState<ShiftSlot | null>(null);
  const [substituteUserId, setSubstituteUserId] = useState<number | null>(null);
  const [overrideStart, setOverrideStart] = useState('');
  const [overrideEnd, setOverrideEnd] = useState('');
  const [overrideReason, setOverrideReason] = useState('');
  const [savingOverride, setSavingOverride] = useState(false);

  const [detailShift, setDetailShift] = useState<ShiftSlot | null>(null);

  const handleTabChange = (nextTab: string) => {
    setSearchParams((prev) => {
      const p = new URLSearchParams(prev);
      if (nextTab === 'board') {
        p.delete('tab');
      } else {
        p.set('tab', nextTab);
      }
      return p;
    });
  };

  // Fetch users & schedules on mount
  const fetchInitialData = useCallback(async () => {
    setLoading(true);
    try {
      const [schedRes, userRes, overridesRes, policiesRes] = await Promise.all([
        listSchedules(),
        listUsers().catch(() => ({ items: [], total: 0 })),
        listPendingOverrides().catch(() => ({ items: [], total: 0 })),
        listEscalationPolicies().catch(() => ({ items: [], total: 0 })),
      ]);
      setSchedules(schedRes.items || []);
      setUsers(userRes.items || []);
      const pendingItems = (overridesRes.items || []).filter((i) => i.status === 'pending');
      setPendingCount(pendingItems.length);
      setEscalationPolicies(policiesRes.items || []);

      if (schedRes.items && schedRes.items.length > 0) {
        setSelectedScheduleId((prev) => prev ?? schedRes.items[0].id);
      }
    } catch (e) {
      console.error('Failed to load on-call data:', e);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchInitialData();
  }, [fetchInitialData]);

  // Compute date range for current view
  const { rangeStart, rangeEnd, rangeLabel } = useMemo(() => {
    const year = currentDate.getFullYear();
    const month = currentDate.getMonth();

    if (viewMode === 'month') {
      const firstDay = new Date(year, month, 1);
      // Monday-first offset
      const startDayOffset = (firstDay.getDay() + 6) % 7;
      const start = new Date(year, month, 1 - startDayOffset, 0, 0, 0);

      const lastDay = new Date(year, month + 1, 0);
      const endDayOffset = (7 - ((lastDay.getDay() + 6) % 7) - 1);
      const end = new Date(year, month + 1, endDayOffset, 23, 59, 59);

      const label = `${year}年 ${month + 1}月`;
      return { rangeStart: start, rangeEnd: end, rangeLabel: label };
    } else if (viewMode === 'week') {
      const curr = new Date(currentDate);
      const dayOffset = (curr.getDay() + 6) % 7;
      const start = new Date(curr.setDate(curr.getDate() - dayOffset));
      start.setHours(0, 0, 0, 0);
      const end = new Date(start);
      end.setDate(end.getDate() + 6);
      end.setHours(23, 59, 59, 999);

      const label = `${start.getMonth() + 1}月${start.getDate()}日 - ${end.getMonth() + 1}月${end.getDate()}日`;
      return { rangeStart: start, rangeEnd: end, rangeLabel: label };
    } else {
      const start = new Date(currentDate);
      start.setHours(0, 0, 0, 0);
      const end = new Date(currentDate);
      end.setHours(23, 59, 59, 999);

      const label = `${year}年${month + 1}月${currentDate.getDate()}日`;
      return { rangeStart: start, rangeEnd: end, rangeLabel: label };
    }
  }, [currentDate, viewMode]);

  // Fetch shifts & live status for selected schedule
  const fetchCalendar = useCallback(async () => {
    if (!selectedScheduleId) return;
    setCalendarLoading(true);
    try {
      const [calRes, curRes] = await Promise.all([
        getCalendarShifts(selectedScheduleId, rangeStart.toISOString(), rangeEnd.toISOString()),
        getCurrentOnCall(selectedScheduleId).catch(() => null),
      ]);
      setShifts(calRes.shifts || []);
      setGaps(calRes.gaps || []);
      setLiveStatus(curRes);
    } catch (e) {
      console.error('Failed to load calendar shifts:', e);
    } finally {
      setCalendarLoading(false);
    }
  }, [selectedScheduleId, rangeStart, rangeEnd]);

  useEffect(() => {
    fetchCalendar();
  }, [fetchCalendar]);

  // Live countdown ticker
  useEffect(() => {
    const updateCountdown = () => {
      if (!liveStatus || !liveStatus.handoff_time) {
        setCountdown('');
        return;
      }
      const handoff = new Date(liveStatus.handoff_time).getTime();
      const now = Date.now();
      const diffSec = Math.floor((handoff - now) / 1000);

      if (diffSec <= 0) {
        setCountdown(tr('已到交接时间', 'Handoff time reached'));
        return;
      }

      const hours = Math.floor(diffSec / 3600);
      const mins = Math.floor((diffSec % 3600) / 60);
      const secs = diffSec % 60;
      setCountdown(
        `${String(hours).padStart(2, '0')}:${String(mins).padStart(2, '0')}:${String(secs).padStart(2, '0')}`
      );
    };

    updateCountdown();
    const timer = setInterval(updateCountdown, 1000);
    return () => clearInterval(timer);
  }, [liveStatus, tr]);

  // Navigation handlers
  const handlePrev = () => {
    const d = new Date(currentDate);
    if (viewMode === 'month') d.setMonth(d.getMonth() - 1);
    else if (viewMode === 'week') d.setDate(d.getDate() - 7);
    else d.setDate(d.getDate() - 1);
    setCurrentDate(d);
  };

  const handleNext = () => {
    const d = new Date(currentDate);
    if (viewMode === 'month') d.setMonth(d.getMonth() + 1);
    else if (viewMode === 'week') d.setDate(d.getDate() + 7);
    else d.setDate(d.getDate() + 1);
    setCurrentDate(d);
  };

  const handleToday = () => {
    setCurrentDate(new Date());
  };

  const handleOpenCreateSchedule = () => {
    setScheduleModalMode('create');
    setScheduleModalOpen(true);
  };

  const handleOpenEditSchedule = () => {
    if (!selectedSchedule) return;
    setScheduleModalMode('edit');
    setScheduleModalOpen(true);
  };

  const handleScheduleSuccess = async (savedScheduleId?: number) => {
    await fetchInitialData();
    if (savedScheduleId) {
      setSelectedScheduleId(savedScheduleId);
    }
    await fetchCalendar();
  };

  const handleScheduleDelete = async (deletedScheduleId: number) => {
    const updated = schedules.filter((s) => s.id !== deletedScheduleId);
    setSchedules(updated);
    if (selectedScheduleId === deletedScheduleId) {
      setSelectedScheduleId(updated[0]?.id || null);
    }
    await fetchInitialData();
    await fetchCalendar();
  };

  // Open override modal
  const handleOpenOverride = (shift: ShiftSlot) => {
    setDetailShift(null);
    setOverrideShift(shift);
    setOverrideStart(toDateTimeLocalValue(shift.start_time));
    setOverrideEnd(toDateTimeLocalValue(shift.end_time));
    setSubstituteUserId(null);
    setOverrideReason('');
    setOverrideModalOpen(true);
  };

  // Submit override
  const handleSubmitOverride = async () => {
    if (!selectedScheduleId || !overrideShift || !substituteUserId) return;
    setSavingOverride(true);
    try {
      await createOverride(selectedScheduleId, {
        rotation_id: overrideShift.rotation_id,
        type: 'override',
        original_user_id: overrideShift.user_id,
        substitute_user_id: substituteUserId,
        start_time: new Date(overrideStart).toISOString(),
        end_time: new Date(overrideEnd).toISOString(),
        reason: overrideReason.trim(),
      });
      setOverrideModalOpen(false);
      if (detailShift) setDetailShift(null);
      await fetchCalendar();
    } catch (e) {
      console.error('Failed to submit override:', e);
    } finally {
      setSavingOverride(false);
    }
  };

  // Render month grid
  const monthDays = useMemo(() => {
    if (viewMode !== 'month') return [];
    const days: Date[] = [];
    const cur = new Date(rangeStart);
    while (cur <= rangeEnd) {
      days.push(new Date(cur));
      cur.setDate(cur.getDate() + 1);
    }
    return days;
  }, [viewMode, rangeStart, rangeEnd]);

  const selectedSchedule = useMemo(
    () => schedules.find((s) => s.id === selectedScheduleId),
    [schedules, selectedScheduleId]
  );

  return (
    <Tabs value={currentTab} onValueChange={handleTabChange} className="contents">
      <main className="anim-fade flex flex-1 min-w-0 flex-col overflow-hidden">
        {/* Page Header */}
        <PageHeader
          title={tr('值班排班大盘', 'On-Call Schedules')}
          subtitle={tr(
            '自动化生产值班轮转、告警标签路由与多通道事件响应大屏',
            'Automated on-call rotations, alert routing, and multi-channel incident response.'
          )}
          actions={
            currentTab === 'board' ? (
              <div className="flex items-center gap-2">
                <Button size="sm" variant="primary" onClick={handleOpenCreateSchedule}>
                  <Plus size={14} className="mr-1.5" />
                  {tr('新建排班计划', 'New Schedule')}
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={fetchCalendar}
                  disabled={calendarLoading}
                >
                  <RefreshCw
                    size={14}
                    className={cn('mr-1.5', calendarLoading && 'animate-spin')}
                  />
                  {tr('刷新', 'Refresh')}
                </Button>
              </div>
            ) : null
          }
          navigation={
            <TabsList className="flex items-center gap-1 overflow-x-auto">
              <TabsTrigger value="board">
                <CalendarDays size={14} className="mr-1.5" />
                {tr('排班大屏', 'Schedule Board')}
              </TabsTrigger>
              <TabsTrigger value="routes">
                <GitFork size={14} className="mr-1.5" />
                {tr('告警路由', 'Route Tree')}
              </TabsTrigger>
              <TabsTrigger value="escalation">
                <BellRing size={14} className="mr-1.5" />
                {tr('通知与响应', 'Escalation & ChatOps')}
              </TabsTrigger>
              <TabsTrigger value="approvals">
                <UserCheck size={14} className="mr-1.5" />
                {tr('换班审批', 'Swap Approvals')}
                {pendingCount > 0 && (
                  <span className="ml-1.5 inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-amber-500 px-1 text-[10px] font-bold text-white">
                    {pendingCount}
                  </span>
                )}
              </TabsTrigger>
              <TabsTrigger value="analytics">
                <Activity size={14} className="mr-1.5" />
                {tr('交接与分析', 'Handoff & Analytics')}
              </TabsTrigger>
              <TabsTrigger value="voice">
                <PhoneCall size={14} className="mr-1.5" />
                {tr('语音网关', 'Voice Gateway')}
              </TabsTrigger>
            </TabsList>
          }
        />

        <div className="flex-1 min-w-0 overflow-y-auto px-6 py-6 space-y-6">
          <TabsContent value="board" className="space-y-6">
          {/* Top Live On-Call Banner Card */}
          <Card className="p-5">
        <div className="flex flex-wrap items-center justify-between gap-4 border-b border-border pb-4">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-indigo-500/10 text-indigo-500">
              <Clock size={20} />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <span className="text-sm font-semibold text-text">
                  {selectedSchedule?.name || tr('生产环境值班', 'Production On-Call')}
                </span>
                <Chip tone="success" dense>
                  {tr('活跃中', 'Active')}
                </Chip>
              </div>
              <div className="text-xs text-text-muted mt-0.5">
                {tr('交接时刻：', 'Handoff time: ')}
                {selectedSchedule?.handoff_time || '09:00:00'} (
                {selectedSchedule?.timezone || 'Asia/Shanghai'})
              </div>
            </div>
          </div>

          {/* Countdown & Next Shift */}
          <div className="flex items-center gap-6">
            {countdown && (
              <div className="text-right">
                <div className="text-xs text-text-muted">
                  {liveStatus?.primary_user
                    ? tr('距离下一次交接班剩余', 'Time until next handoff')
                    : tr('距离下个班次开始', 'Time until next shift')}
                </div>
                <div className="text-xl font-mono font-bold tracking-tight text-indigo-600 dark:text-indigo-400">
                  {countdown}
                </div>
              </div>
            )}
            {liveStatus?.next_shift && (
              <div className="hidden sm:block border-l border-border pl-6 text-left">
                <div className="text-xs text-text-muted">{tr('接班人预览', 'Next on-call')}</div>
                <div className="text-sm font-medium text-text mt-0.5">
                  {liveStatus.next_shift.user_name || `User #${liveStatus.next_shift.user_id}`}
                </div>
                <div className="text-xs text-text-faint">
                  {new Date(liveStatus.next_shift.start_time).toLocaleDateString()}
                </div>
              </div>
            )}
          </div>
        </div>

        {/* Current Personnel Cards */}
        <div className="mt-4 grid grid-cols-1 md:grid-cols-2 gap-4">
          {/* Primary */}
          <div className="flex items-center justify-between rounded-lg border border-border bg-bg/50 p-3.5">
            <div className="flex items-center gap-3">
              <div className="relative flex h-10 w-10 items-center justify-center rounded-full bg-indigo-500/10 text-indigo-600 font-semibold text-sm">
                {liveStatus?.primary_user?.user_name?.[0] || '1'}
                <span className="absolute bottom-0 right-0 h-2.5 w-2.5 rounded-full bg-emerald-500 ring-2 ring-card" />
              </div>
              <div>
                <div className="flex items-center gap-2">
                  <span className="text-xs font-semibold text-text-muted uppercase">
                    {tr('一线主值班人 (Primary)', 'Primary On-Call')}
                  </span>
                  {liveStatus?.primary_user?.is_override && (
                    <Chip tone="warning" dense>
                      {tr('代班中', 'Override')}
                    </Chip>
                  )}
                </div>
                <div className="text-base font-semibold text-text mt-0.5">
                  {liveStatus?.primary_user?.user_name || tr('暂无主值班人', 'No Primary Assigned')}
                </div>
                {liveStatus?.primary_user?.is_override && liveStatus.primary_user.end_time && (
                  <div className="text-xs text-amber-600 dark:text-amber-400 mt-0.5 font-medium">
                    {tr('代班至 ', 'Override until ')}
                    {formatTime(liveStatus.primary_user.end_time)}
                    {liveStatus.primary_user.original_user_name && (
                      <span className="text-text-muted ml-1 font-normal">
                        ({tr('原值班人: ', 'Original: ')}{liveStatus.primary_user.original_user_name})
                      </span>
                    )}
                  </div>
                )}
                {liveStatus?.primary_user?.user_phone && (
                  <div className="text-xs text-text-muted">
                    {liveStatus.primary_user.user_phone}
                  </div>
                )}
              </div>
            </div>
            {liveStatus?.primary_user && (
              <Button
                size="sm"
                variant="outline"
                onClick={() => handleOpenOverride(liveStatus.primary_user!)}
              >
                {tr('申请换班/代班', 'Swap/Override')}
              </Button>
            )}
          </div>

          {/* Secondary */}
          <div className="flex items-center justify-between rounded-lg border border-border bg-bg/50 p-3.5">
            <div className="flex items-center gap-3">
              <div className="flex h-10 w-10 items-center justify-center rounded-full bg-sky-500/10 text-sky-600 dark:text-sky-400 font-semibold text-sm">
                <ShieldCheck size={20} />
              </div>
              <div>
                <div className="flex items-center gap-2">
                  <span className="text-xs font-semibold text-text-muted uppercase">
                    {tr('二线专家支持 (Secondary)', 'Secondary Support')}
                  </span>
                  {liveStatus?.secondary_user ? (
                    <Chip tone="accent" dense>
                      {tr('已就绪', 'Ready')}
                    </Chip>
                  ) : (
                    <Chip tone="default" dense>
                      {tr('未配置', 'Unset')}
                    </Chip>
                  )}
                </div>
                <div className="text-base font-semibold text-text mt-0.5">
                  {liveStatus?.secondary_user?.user_name || tr('未配置二线专家', 'No Secondary Specialist')}
                </div>
                {liveStatus?.secondary_user?.user_phone && (
                  <div className="text-xs text-text-muted">
                    {liveStatus.secondary_user.user_phone}
                  </div>
                )}
                {!liveStatus?.secondary_user && (
                  <div className="text-[11px] text-text-faint">
                    {tr('一线超时或重大突发时，系统将升级至二线专家梯队响应', 'Escalation tier for timeouts and major incidents')}
                  </div>
                )}
              </div>
            </div>

            <div className="flex items-center gap-2">
              {liveStatus?.secondary_user ? (
                <>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => setSecondaryModalOpen(true)}
                  >
                    <Settings size={13} className="mr-1 text-sky-600 dark:text-sky-400" />
                    {tr('调整二线配置', 'Edit Secondary')}
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => handleOpenOverride(liveStatus.secondary_user!)}
                  >
                    {tr('申请换班/代班', 'Swap/Override')}
                  </Button>
                </>
              ) : (
                <Button
                  size="sm"
                  variant="primary"
                  onClick={() => setSecondaryModalOpen(true)}
                >
                  <ShieldCheck size={14} className="mr-1.5" />
                  {tr('配置二线专家支持', 'Configure Secondary')}
                </Button>
              )}
            </div>
          </div>
        </div>
      </Card>

      {/* Schedule Picker & Calendar Toolbar */}
      <div className="flex flex-wrap items-center justify-between gap-4">
        {/* Schedule Tabs */}
        <div className="flex items-center gap-2 overflow-x-auto">
          {schedules.map((s) => (
            <button
              key={s.id}
              onClick={() => setSelectedScheduleId(s.id)}
              className={cn(
                'rounded-lg px-3.5 py-1.5 text-xs font-medium transition-colors border',
                selectedScheduleId === s.id
                  ? 'border-indigo-500 bg-indigo-50 dark:bg-indigo-950/40 text-indigo-700 dark:text-indigo-300 shadow-sm'
                  : 'border-border bg-card text-text-muted hover:bg-bg hover:text-text'
              )}
            >
              {s.name}
            </button>
          ))}
          {selectedSchedule && (
            <Button
              size="sm"
              variant="outline"
              onClick={handleOpenEditSchedule}
              className="text-xs shrink-0"
              title={tr('编辑当前排班基础信息与轮转池', 'Edit schedule & rotations')}
            >
              <Settings size={13} className="mr-1 text-text-muted" />
              {tr('编辑排班', 'Edit Schedule')}
            </Button>
          )}
          {schedules.length === 0 && !loading && (
            <span className="text-xs text-text-faint">{tr('暂无排班计划', 'No schedules found')}</span>
          )}
        </div>

        {/* View Switcher & Navigation */}
        <div className="flex items-center gap-3">
          <div className="flex items-center rounded-lg border border-border bg-card p-0.5">
            <Button
              size="sm"
              variant={viewMode === 'month' ? 'primary' : 'ghost'}
              onClick={() => setViewMode('month')}
            >
              {tr('月视图', 'Month')}
            </Button>
            <Button
              size="sm"
              variant={viewMode === 'week' ? 'primary' : 'ghost'}
              onClick={() => setViewMode('week')}
            >
              {tr('周视图', 'Week')}
            </Button>
            <Button
              size="sm"
              variant={viewMode === 'day' ? 'primary' : 'ghost'}
              onClick={() => setViewMode('day')}
            >
              {tr('日视图', 'Day')}
            </Button>
          </div>

          <div className="flex items-center gap-1.5">
            <Button size="icon" variant="outline" onClick={handlePrev}>
              <ChevronLeft size={16} />
            </Button>
            <Button size="sm" variant="outline" onClick={handleToday}>
              {tr('今天', 'Today')}
            </Button>
            <Button size="icon" variant="outline" onClick={handleNext}>
              <ChevronRight size={16} />
            </Button>
            <span className="text-sm font-semibold text-text ml-2 min-w-[120px] text-center">
              {rangeLabel}
            </span>
          </div>
        </div>
      </div>

      {/* Schedule Gap Alert Banner (if any gaps detected) */}
      {gaps.length > 0 && (
        <div className="flex items-center gap-3 rounded-lg border border-red-500/30 bg-red-50 dark:bg-red-950/20 px-4 py-3 text-red-700 dark:text-red-300">
          <AlertTriangle size={18} className="shrink-0 text-red-600 dark:text-red-400" />
          <div className="text-xs">
            <span className="font-semibold">
              {tr('检测到排班空洞预警：', 'Schedule Gap Detected: ')}
            </span>
            {tr(
              `未来周期存在 ${gaps.length} 个无人值班时间段，请及时补位以防生产事件漏单！`,
              `There are ${gaps.length} gaps in coverage. Please assign on-call engineers to ensure continuous coverage.`
            )}
          </div>
        </div>
      )}

      {/* Calendar Grid Board */}
      <Card className="!p-0 overflow-hidden">
        {calendarLoading ? (
          <div className="flex h-96 items-center justify-center">
            <RefreshCw size={24} className="animate-spin text-indigo-500" />
          </div>
        ) : schedules.length === 0 ? (
          <EmptyState
            icon={CalendarDays}
            title={tr('尚未创建排班计划', 'No on-call schedule configured')}
            hint={tr(
              '点击右上角【新建排班计划】，配置人员轮转池与交接时刻。',
              'Click "New Schedule" to add rotations and assign engineers.'
            )}
            action={
              <Button size="sm" variant="primary" onClick={handleOpenCreateSchedule}>
                <Plus size={14} className="mr-1.5" />
                {tr('新建排班计划', 'New Schedule')}
              </Button>
            }
          />
        ) : viewMode === 'month' ? (
          <div className="w-full">
            {/* Weekday headers */}
            <div className="grid grid-cols-7 border-b border-border bg-bg/50 text-center text-xs font-semibold text-text-muted py-2.5">
              <span>{tr('周一', 'Mon')}</span>
              <span>{tr('周二', 'Tue')}</span>
              <span>{tr('周三', 'Wed')}</span>
              <span>{tr('周四', 'Thu')}</span>
              <span>{tr('周五', 'Fri')}</span>
              <span>{tr('周六', 'Sat')}</span>
              <span>{tr('周日', 'Sun')}</span>
            </div>

            {/* Month Days Cells */}
            <div className="grid grid-cols-7 divide-x divide-y divide-border">
              {monthDays.map((day, idx) => {
                const isCurrentMonth = day.getMonth() === currentDate.getMonth();
                const isToday =
                  day.toDateString() === new Date().toDateString();

                // Find shifts overlapping this day
                const dayStart = new Date(day);
                dayStart.setHours(0, 0, 0, 0);
                const dayEnd = new Date(day);
                dayEnd.setHours(23, 59, 59, 999);

                const dayShifts = shifts.filter((s) => {
                  const sStart = new Date(s.start_time);
                  const sEnd = new Date(s.end_time);
                  return sStart <= dayEnd && sEnd >= dayStart;
                });

                // Consolidate shifts for the day: if a user is continuously on call across the midnight boundary,
                // we display a single consolidated pill for that user/tier unless there is an override.
                const consolidatedShifts = dayShifts.reduce<ShiftSlot[]>((acc, curr) => {
                  const existing = acc.find(
                    (s) => s.user_id === curr.user_id && s.tier === curr.tier
                  );
                  if (!existing) {
                    acc.push(curr);
                  } else if (curr.is_override && !existing.is_override) {
                    const idx = acc.indexOf(existing);
                    acc[idx] = curr;
                  }
                  return acc;
                }, []);

                // Find gaps overlapping this day
                const dayGaps = gaps.filter((g) => {
                  const gStart = new Date(g.start_time);
                  const gEnd = new Date(g.end_time);
                  return gStart <= dayEnd && gEnd >= dayStart;
                });

                return (
                  <div
                    key={idx}
                    className={cn(
                      'min-h-[110px] p-2 transition-colors flex flex-col',
                      !isCurrentMonth && 'bg-bg/30 text-text-faint',
                      isToday && 'bg-indigo-50/20 dark:bg-indigo-950/10'
                    )}
                  >
                    <div className="flex items-center justify-between mb-1.5">
                      <span
                        className={cn(
                          'text-xs font-medium inline-flex h-5 w-5 items-center justify-center rounded-full',
                          isToday && 'bg-indigo-600 text-white font-bold'
                        )}
                      >
                        {day.getDate()}
                      </span>
                    </div>

                    {/* Shifts Pills */}
                    <div className="flex flex-col gap-1 flex-1 overflow-hidden">
                      {consolidatedShifts.map((s, sIdx) => {
                        const isPrimary = s.tier === 1;
                        const hasMultipleInTier =
                          consolidatedShifts.filter((cs) => cs.tier === s.tier).length > 1;
                        const sStart = new Date(s.start_time);
                        const sEnd = new Date(s.end_time);
                        const isStartingToday = sStart >= dayStart && sStart <= dayEnd;
                        const isEndingToday = sEnd >= dayStart && sEnd <= dayEnd;

                        return (
                          <div
                            key={sIdx}
                            onClick={() => setDetailShift(s)}
                            className={cn(
                              'cursor-pointer rounded px-2 py-1 text-[11px] truncate transition-colors hover:opacity-85 border-l-2 shadow-xs',
                              s.is_override
                                ? 'border-amber-500 bg-amber-500/10 text-amber-900 dark:text-amber-200'
                                : isPrimary
                                  ? 'border-indigo-500 bg-indigo-500/10 text-indigo-900 dark:text-indigo-200 font-medium'
                                  : 'border-zinc-400 bg-zinc-500/10 text-zinc-800 dark:text-zinc-300'
                            )}
                            title={`${s.user_name} (${isPrimary ? '一线' : '二线'})\n${formatTime(s.start_time)} - ${formatTime(s.end_time)}`}
                          >
                            <span className="font-semibold mr-1">
                              {isPrimary ? '①' : '②'}
                            </span>
                            {s.user_name || `User #${s.user_id}`}
                            {s.is_override && (
                              <span className="ml-1 text-[10px] text-amber-600 font-bold">
                                [{tr('代', 'Sub')}]
                              </span>
                            )}
                            {hasMultipleInTier && isEndingToday && !isStartingToday && (
                              <span className="ml-1 text-[10px] text-text-muted">
                                ({tr('至', 'to')} {formatTime(s.end_time)})
                              </span>
                            )}
                            {hasMultipleInTier && isStartingToday && (
                              <span className="ml-1 text-[10px] text-text-muted">
                                ({tr('起', 'from')} {formatTime(s.start_time)})
                              </span>
                            )}
                          </div>
                        );
                      })}

                      {/* Gaps Warnings */}
                      {dayGaps.map((g, gIdx) => (
                        <div
                          key={gIdx}
                          className="rounded border border-red-500/30 bg-red-500/10 px-1.5 py-0.5 text-[10px] text-red-600 dark:text-red-400 font-medium flex items-center gap-1"
                        >
                          <AlertTriangle size={10} className="shrink-0" />
                          <span className="truncate">{tr('班次空缺', 'Gap')}</span>
                        </div>
                      ))}
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
        ) : (
          /* Week & Day Timeline View */
          <div className="p-4 divide-y divide-border">
            {shifts.map((s, idx) => (
              <div
                key={idx}
                onClick={() => setDetailShift(s)}
                className="flex items-center justify-between py-3 hover:bg-bg/40 px-2 rounded-lg cursor-pointer transition-colors"
              >
                <div className="flex items-center gap-3">
                  <div
                    className={cn(
                      'flex h-8 w-8 items-center justify-center rounded-full text-xs font-bold',
                      s.tier === 1
                        ? 'bg-indigo-500/10 text-indigo-600'
                        : 'bg-zinc-500/10 text-zinc-600'
                    )}
                  >
                    {s.tier === 1 ? '1' : '2'}
                  </div>
                  <div>
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-semibold text-text">
                        {s.user_name || `User #${s.user_id}`}
                      </span>
                      <Chip tone={s.tier === 1 ? 'accent' : 'default'} dense>
                        {s.tier === 1 ? tr('一线', 'Primary') : tr('二线', 'Secondary')}
                      </Chip>
                      {s.is_override && (
                        <Chip tone="warning" dense>
                          {tr('代班覆盖', 'Override')}
                        </Chip>
                      )}
                    </div>
                    <div className="text-xs text-text-muted mt-0.5">
                      {new Date(s.start_time).toLocaleString()} ~{' '}
                      {new Date(s.end_time).toLocaleString()}
                    </div>
                  </div>
                </div>

                <div className="flex items-center gap-2">
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={(e) => {
                      e.stopPropagation();
                      handleOpenOverride(s);
                    }}
                  >
                    {tr('申请换班', 'Request Swap')}
                  </Button>
                </div>
              </div>
            ))}
            {shifts.length === 0 && (
              <div className="py-12 text-center text-xs text-text-muted">
                {tr('该时间段内暂无班次记录', 'No shifts scheduled in this time frame.')}
              </div>
            )}
          </div>
        )}
      </Card>
        </TabsContent>

        <TabsContent value="routes">
          <RouteTreeTab schedules={schedules} escalationPolicies={escalationPolicies} />
        </TabsContent>

        <TabsContent value="escalation">
          <EscalationChatOpsTab schedules={schedules} />
        </TabsContent>

        <TabsContent value="approvals">
          <SwapApprovalsTab onPendingCountChange={setPendingCount} />
        </TabsContent>

        <TabsContent value="analytics">
          <HandoffAnalyticsTab schedules={schedules} />
        </TabsContent>

        <TabsContent value="voice">
          <VoiceGatewayTab />
        </TabsContent>
      </div>
    </main>

      {/* Schedule Edit / Create Modal */}
      <ScheduleEditModal
        open={scheduleModalOpen}
        onClose={() => setScheduleModalOpen(false)}
        mode={scheduleModalMode}
        initialSchedule={scheduleModalMode === 'edit' ? selectedSchedule : null}
        users={users}
        onSuccess={handleScheduleSuccess}
        onDelete={handleScheduleDelete}
      />

      {/* Secondary Specialist Rotation Modal */}
      <SecondaryRotationModal
        open={secondaryModalOpen}
        onClose={() => setSecondaryModalOpen(false)}
        schedule={selectedSchedule || null}
        existingRotation={selectedSchedule?.rotations?.find((r) => r.tier === 2) || null}
        users={users}
        onSuccess={async () => {
          await fetchInitialData();
          await fetchCalendar();
        }}
      />

      {/* Override / Shift Swap Modal */}
      <Modal
        open={overrideModalOpen}
        onClose={() => setOverrideModalOpen(false)}
        title={tr('申请代班 / 换班覆盖 (Override)', 'Shift Swap / Override')}
        size="md"
        footer={
          <div className="flex justify-end gap-2">
            <Button size="sm" variant="outline" onClick={() => setOverrideModalOpen(false)}>
              {tr('取消', 'Cancel')}
            </Button>
            <Button
              size="sm"
              variant="primary"
              onClick={handleSubmitOverride}
              disabled={savingOverride || !substituteUserId}
            >
              {savingOverride ? tr('提交中...', 'Submitting...') : tr('确认代班', 'Confirm')}
            </Button>
          </div>
        }
      >
        <div className="space-y-4 py-1">
          <div className="rounded-lg border border-border bg-bg/50 p-3 text-xs space-y-1">
            <div className="text-text-muted">
              {tr('原定值班人：', 'Original Assignee: ')}
              <span className="font-semibold text-text">
                {overrideShift?.user_name || `User #${overrideShift?.user_id}`}
              </span>
            </div>
            <div className="text-text-muted">
              {tr('所属排班：', 'Schedule: ')}
              <span className="font-semibold text-text">{overrideShift?.schedule_name}</span>
            </div>
          </div>

          <div>
            <Label className="text-xs mb-1 block">{tr('选择代班 / 接班工程师 *', 'Substitute Engineer *')}</Label>
            <Select
              value={substituteUserId ? String(substituteUserId) : ''}
              onValueChange={(val) => setSubstituteUserId(val ? Number(val) : null)}
              options={[
                { value: '', label: tr('-- 请选择代班接班人 --', '-- Select user --'), disabled: true },
                ...users
                  .filter((u) => u.id !== overrideShift?.user_id)
                  .map((u) => ({
                    value: String(u.id),
                    label: `${u.display_name || u.email}${u.phone ? ` (${u.phone})` : ''}`,
                  })),
              ]}
              className="w-full"
            />
            {users.filter((u) => u.id !== overrideShift?.user_id).length === 0 && (
              <div className="mt-1.5 text-[11px] text-amber-600 dark:text-amber-400">
                {tr(
                  '提示：当前系统中暂无其他可选工程师。请先在【用户管理】中添加更多团队成员。',
                  'Notice: No other users available. Please add more members in User Management first.'
                )}
              </div>
            )}
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div>
              <Label className="text-xs">{tr('开始时间', 'Start Time')}</Label>
              <Input
                type="datetime-local"
                value={overrideStart}
                onChange={(e) => setOverrideStart(e.target.value)}
                className="mt-1 text-xs"
              />
            </div>
            <div>
              <Label className="text-xs">{tr('结束时间', 'End Time')}</Label>
              <Input
                type="datetime-local"
                value={overrideEnd}
                onChange={(e) => setOverrideEnd(e.target.value)}
                className="mt-1 text-xs"
              />
            </div>
          </div>

          <div>
            <Label className="text-xs">{tr('换班事由', 'Reason')}</Label>
            <Textarea
              value={overrideReason}
              onChange={(e) => setOverrideReason(e.target.value)}
              placeholder="e.g. 紧急事假外出 4 小时，由李四临时顶替接单"
              className="mt-1 text-xs"
              rows={2}
            />
          </div>
        </div>
      </Modal>

      {/* Shift Details Modal */}
      <Modal
        open={Boolean(detailShift)}
        onClose={() => setDetailShift(null)}
        title={tr('班次详情', 'Shift Details')}
        size="sm"
        footer={
          <div className="flex justify-between w-full items-center">
            {detailShift && (
              <Button
                size="sm"
                variant="outline"
                onClick={() => {
                  handleOpenOverride(detailShift);
                }}
              >
                {tr('申请代班', 'Swap Shift')}
              </Button>
            )}
            <Button size="sm" variant="ghost" onClick={() => setDetailShift(null)}>
              {tr('关闭', 'Close')}
            </Button>
          </div>
        }
      >
        {detailShift && (
          <div className="space-y-3 py-1 text-xs">
            <div className="flex items-center justify-between border-b border-border pb-2">
              <span className="text-text-muted">{tr('值班人', 'Assignee')}</span>
              <span className="font-semibold text-text text-sm">
                {detailShift.user_name || `User #${detailShift.user_id}`}
              </span>
            </div>
            <div className="flex items-center justify-between border-b border-border pb-2">
              <span className="text-text-muted">{tr('梯队级别', 'Tier')}</span>
              <Chip tone={detailShift.tier === 1 ? 'accent' : 'default'} dense>
                {detailShift.tier === 1 ? tr('一线主值班人', 'Primary Tier') : tr('二线支持', 'Secondary Tier')}
              </Chip>
            </div>
            <div className="flex items-center justify-between border-b border-border pb-2">
              <span className="text-text-muted">{tr('排班计划', 'Schedule')}</span>
              <span className="text-text">{detailShift.schedule_name}</span>
            </div>
            <div className="flex items-center justify-between border-b border-border pb-2">
              <span className="text-text-muted">{tr('联系电话', 'Phone')}</span>
              <span className="text-text font-mono">{detailShift.user_phone || tr('未登记', 'None')}</span>
            </div>
            <div className="flex items-center justify-between border-b border-border pb-2">
              <span className="text-text-muted">{tr('起止时间', 'Duration')}</span>
              <span className="text-text font-mono text-right">
                {new Date(detailShift.start_time).toLocaleString()}<br />
                ~ {new Date(detailShift.end_time).toLocaleString()}
              </span>
            </div>
            {detailShift.is_override && (
              <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-2 text-amber-800 dark:text-amber-200">
                <span className="font-semibold">{tr('代班信息：', 'Override Info: ')}</span>
                {tr('原值班人 ', 'Original: ')}
                {detailShift.original_user_name || detailShift.original_user_id}
                {detailShift.reason && ` (事由: ${detailShift.reason})`}
              </div>
            )}
          </div>
        )}
      </Modal>
    </Tabs>
  );
}
