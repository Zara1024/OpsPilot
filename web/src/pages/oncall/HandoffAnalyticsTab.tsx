import { useState, useEffect, useCallback } from 'react';
import {
  FileText,
  AlertTriangle,
  HeartPulse,
  CheckCircle2,
  TrendingDown,
  RefreshCw,
  CheckSquare,
  Bell,
} from 'lucide-react';
import {
  Button,
  Card,
  Chip,
  EmptyState,
  Label,
  Textarea,
} from '@/components/ui';
import { useDialogs } from '@/components/ui/useDialogs';
import { useI18n } from '@/i18n/locale';
import {
  getCurrentHandoff,
  listHandoffLogs,
  ackHandoff,
  sendHandoffReminderNow,
  getBurnoutAnalytics,
  getNoisyAlerts,
  type CurrentHandoffStatus,
  type HandoffLogItem,
  type BurnoutAnalytics,
  type NoisyAlertItem,
  type OnCallSchedule,
} from '@/api/oncall';

interface HandoffAnalyticsTabProps {
  schedules: OnCallSchedule[];
}

export function HandoffAnalyticsTab({ schedules: _schedules }: HandoffAnalyticsTabProps) {
  const { tr } = useI18n();
  const { alertAction, dialog } = useDialogs();

  const [handoff, setHandoff] = useState<CurrentHandoffStatus | null>(null);
  const [handoffLogs, setHandoffLogs] = useState<HandoffLogItem[]>([]);
  const [analytics, setAnalytics] = useState<BurnoutAnalytics | null>(null);
  const [noisyAlerts, setNoisyAlerts] = useState<NoisyAlertItem[]>([]);
  const [loading, setLoading] = useState(true);

  // Handoff signing & reminder
  const [handoffNotes, setHandoffNotes] = useState('');
  const [signingHandoff, setSigningHandoff] = useState(false);
  const [signSuccess, setSignSuccess] = useState(false);
  const [remindingNow, setRemindingNow] = useState(false);
  const [remindFeedback, setRemindFeedback] = useState<{ ok: boolean; text: string } | null>(null);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const [hRes, lRes, aRes, nRes] = await Promise.all([
        getCurrentHandoff().catch(() => null),
        listHandoffLogs(undefined, 20).catch(() => ({ items: [], total: 0 })),
        getBurnoutAnalytics().catch(() => null),
        getNoisyAlerts().catch(() => ({ items: [] })),
      ]);
      setHandoff(hRes);
      if (hRes?.notes) setHandoffNotes(hRes.notes);
      setHandoffLogs(lRes?.items || []);
      setAnalytics(aRes);
      setNoisyAlerts(nRes?.items || []);
    } catch (e) {
      console.error('Failed to load handoff & analytics:', e);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const handleSignHandoff = async () => {
    if (!handoff) return;
    setSigningHandoff(true);
    setSignSuccess(false);
    try {
      const res = await ackHandoff({
        schedule_id: handoff.schedule_id,
        notes: handoffNotes.trim(),
      });
      setHandoff(res);
      const logsRes = await listHandoffLogs(undefined, 20).catch(() => null);
      if (logsRes?.items) {
        setHandoffLogs(logsRes.items);
      }
      setSignSuccess(true);
      setTimeout(() => setSignSuccess(false), 4000);
    } catch (e: unknown) {
      console.error('Failed to ack handoff:', e);
      await alertAction(e instanceof Error ? e.message : tr('交接确认失败', 'Failed to sign handoff'));
    } finally {
      setSigningHandoff(false);
    }
  };

  const handleRemindNow = async () => {
    if (!handoff || remindingNow) return;
    setRemindingNow(true);
    setRemindFeedback(null);
    try {
      const res = await sendHandoffReminderNow(handoff.schedule_id);
      const logsRes = await listHandoffLogs(undefined, 20).catch(() => null);
      if (logsRes?.items) {
        setHandoffLogs(logsRes.items);
      }
      if (res.ok) {
        setRemindFeedback({
          ok: true,
          text: tr(
            `接班提醒已通过 ${res.channel_type || 'IM'} 发送给 ${handoff.incoming_user_name || '接班人'}`,
            `Handoff reminder sent via ${res.channel_type || 'IM'} to ${handoff.incoming_user_name || 'incoming engineer'}`
          ),
        });
      } else {
        setRemindFeedback({
          ok: false,
          text:
            res.error_message ||
            tr('接班提醒投递失败，请检查通知通道配置', 'Failed to deliver handoff reminder'),
        });
      }
    } catch (e: unknown) {
      setRemindFeedback({
        ok: false,
        text: e instanceof Error ? e.message : tr('发送接班提醒失败', 'Failed to send handoff reminder'),
      });
    } finally {
      setRemindingNow(false);
    }
  };

  return (
    <div className="space-y-8">
      {/* 1. Shift Handoff & Checklist Card */}
      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <FileText className="text-indigo-500" size={18} />
            <h2 className="text-base font-semibold text-text">
              {tr('值班交接仪式与备忘录', 'Shift Handoff Summary')}
            </h2>
          </div>
          <Button
            size="sm"
            variant="ghost"
            onClick={fetchData}
            disabled={loading}
            aria-label={tr('刷新交接与分析数据', 'Refresh handoff and analytics')}
            title={tr('刷新交接与分析数据', 'Refresh handoff and analytics')}
          >
            <RefreshCw size={14} className={loading ? 'animate-spin' : ''} />
          </Button>
        </div>

        {signSuccess && (
          <div className="rounded-lg bg-emerald-500/10 border border-emerald-500/30 px-3 py-2 text-xs text-emerald-600 dark:text-emerald-400">
            {tr('已成功签署确认交接班备忘录！交接责任完成归档。', 'Shift handoff signed and archived successfully!')}
          </div>
        )}

        <Card className="p-5">
          {handoff ? (
            <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
              {/* Left: Handoff Personnel & Time */}
              <div className="space-y-4 lg:border-r lg:border-border lg:pr-6">
                <div>
                  <div className="text-xs text-text-muted">{tr('所属排班计划', 'Schedule')}</div>
                  <div className="text-sm font-semibold text-text mt-0.5">
                    {handoff.schedule_name}
                  </div>
                </div>

                <div className="flex items-center justify-between rounded-lg border border-border bg-bg/50 p-3 text-xs">
                  <div>
                    <span className="text-text-muted">{tr('交班工程师', 'Outgoing')}</span>
                    <div className="font-semibold text-text mt-0.5">{handoff.outgoing_user_name}</div>
                  </div>
                  <span className="text-text-muted">➔</span>
                  <div>
                    <span className="text-text-muted">{tr('接班工程师', 'Incoming')}</span>
                    <div className="font-semibold text-indigo-600 dark:text-indigo-400 mt-0.5">
                      {handoff.incoming_user_name}
                    </div>
                  </div>
                </div>

                <div className="text-xs space-y-1.5 text-text-muted">
                  <div className="flex justify-between">
                    <span>{tr('当前班次区间：', 'Shift Interval: ')}</span>
                    <span className="font-mono text-text">
                      {handoff.shift_start ? new Date(handoff.shift_start).toLocaleDateString() : '-'} ~ {handoff.shift_end ? new Date(handoff.shift_end).toLocaleDateString() : '-'}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span>{tr('交接时刻：', 'Handoff Time: ')}</span>
                    <span className="font-mono text-indigo-600 dark:text-indigo-400 font-bold">
                      {handoff.next_handoff_time || '-'}
                    </span>
                  </div>
                  <div className="flex justify-between items-center">
                    <span>{tr('交接状态：', 'Handoff Status: ')}</span>
                    <Chip tone={handoff.status === 'signed_off' ? 'success' : 'warning'} dense>
                      {handoff.status === 'signed_off' ? tr('已签署', 'Signed') : tr('待交接签署', 'Pending Sign')}
                    </Chip>
                  </div>
                </div>
              </div>

              {/* Middle: Active incidents & checklist */}
              <div className="space-y-4 lg:border-r lg:border-border lg:pr-6">
                <div>
                  <span className="text-xs font-semibold text-text uppercase tracking-wider block mb-2">
                    {tr('未恢复告警与观察项', 'Pending Incidents & Observations')}
                  </span>
                  {handoff.active_incidents_summary && handoff.active_incidents_summary.length > 0 ? (
                    <ul className="space-y-1.5 text-xs">
                      {handoff.active_incidents_summary.map((sum, idx) => (
                        <li key={idx} className="flex items-start gap-2 text-text">
                          <AlertTriangle size={14} className="text-amber-500 shrink-0 mt-0.5" />
                          <span>{sum}</span>
                        </li>
                      ))}
                    </ul>
                  ) : (
                    <div className="text-xs text-text-muted">{tr('无挂起事故，系统运行平稳', 'No hanging incidents')}</div>
                  )}
                </div>

                {handoff.pending_checklist && handoff.pending_checklist.length > 0 && (
                  <div>
                    <span className="text-xs font-semibold text-text uppercase tracking-wider block mb-2">
                      {tr('交接班检查清单', 'Handoff Checklist')}
                    </span>
                    <div className="space-y-1.5 text-xs">
                      {handoff.pending_checklist.map((chk) => (
                        <div key={chk.id} className="flex items-center gap-2 text-text">
                          <CheckCircle2 size={14} className={chk.completed ? 'text-emerald-500' : 'text-text-muted'} />
                          <span className={chk.completed ? 'line-through text-text-muted' : ''}>{chk.title}</span>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
              </div>

              {/* Right: Handoff notes & Sign action */}
              <div className="space-y-3 flex flex-col justify-between">
                <div>
                  <Label className="text-xs block mb-1">
                    {tr('交接备忘录与注意事项', 'Handoff Notes')}
                  </Label>
                  <Textarea
                    rows={4}
                    value={handoffNotes}
                    onChange={(e) => setHandoffNotes(e.target.value)}
                    placeholder={tr('如：夜间清理了 /var/log 磁盘，观察慢 SQL 抖动等...', 'e.g. Cleaned /var/log, monitor slow queries...')}
                    className="w-full text-xs"
                  />
                </div>

                <div className="space-y-2">
                  {signSuccess && (
                    <div className="rounded-md border border-emerald-500/30 bg-emerald-500/10 px-2.5 py-1.5 text-xs text-emerald-700 dark:text-emerald-300">
                      {tr('交接班备忘录已签署归档。', 'Handoff notes signed and archived.')}
                    </div>
                  )}
                  {remindFeedback && (
                    <div
                      className={`rounded-md border px-2.5 py-1.5 text-xs ${
                        remindFeedback.ok
                          ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
                          : 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300'
                      }`}
                    >
                      {remindFeedback.text}
                    </div>
                  )}
                  <div className="flex flex-wrap gap-2">
                    <Button
                      size="sm"
                      variant="primary"
                      onClick={handleSignHandoff}
                      disabled={signingHandoff}
                      className="flex-1 min-w-[140px]"
                    >
                      <CheckSquare size={14} className="mr-1.5" />
                      {signingHandoff ? tr('签署中...', 'Signing...') : tr('确认签署交接备忘录', 'Sign Handoff')}
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={handleRemindNow}
                      disabled={remindingNow}
                      className="flex-1 min-w-[140px]"
                    >
                      <Bell size={14} className="mr-1.5" />
                      {remindingNow ? tr('发送中...', 'Sending...') : tr('立即发送接班提醒', 'Send Handoff Reminder Now')}
                    </Button>
                  </div>
                </div>
              </div>
            </div>
          ) : (
            <div className="py-8 text-center text-xs text-text-muted">
              {tr('暂无交接班数据', 'No handoff data available')}
            </div>
          )}
        </Card>
      </section>

      {/* 2. SRE Burnout Health Analytics */}
      <section className="space-y-3">
        <div className="flex items-center gap-2">
          <HeartPulse className="text-emerald-500" size={18} />
          <h2 className="text-base font-semibold text-text">
            {tr('SRE 疲劳度健康大盘', 'SRE Burnout Health Analytics')}
          </h2>
        </div>

        {/* 4 Stat Cards */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
          <Card className="p-4">
            <span className="text-xs text-text-muted block">
              {tr('MTTA 平均响应时长', 'MTTA (Response Time)')}
            </span>
            <div className="text-2xl font-bold font-mono text-text mt-1">
              {analytics?.mtta_seconds || 0}s
            </div>
            <div className="text-[11px] text-emerald-600 dark:text-emerald-400 mt-1">
              {tr('评级：优秀', 'Rating: Good')}
            </div>
          </Card>

          <Card className="p-4">
            <span className="text-xs text-text-muted block">
              {tr('夜间叫醒电话次数', 'Night Phone Calls')}
            </span>
            <div className="text-2xl font-bold font-mono text-amber-600 dark:text-amber-400 mt-1">
              {analytics?.night_call_count || 0}
            </div>
            <div className="text-[11px] text-text-muted mt-1">
              {tr('近 14 天累计', 'Past 14 days')}
            </div>
          </Card>

          <Card className="p-4">
            <span className="text-xs text-text-muted block">
              {tr('团队健康指数', 'Team Burnout Index')}
            </span>
            <div className="text-2xl font-bold font-mono text-indigo-600 dark:text-indigo-400 mt-1">
              {analytics?.burnout_index || 90}/100
            </div>
            <div className="text-[11px] text-emerald-600 dark:text-emerald-400 mt-1">
              {tr('健康度良好', 'Healthy Status')}
            </div>
          </Card>

          <Card className="p-4">
            <span className="text-xs text-text-muted block">
              {tr('告警风暴降噪比率', 'Noise Reduction Ratio')}
            </span>
            <div className="text-2xl font-bold font-mono text-emerald-600 dark:text-emerald-400 mt-1">
              {analytics?.noise_reduction_ratio || 85}%
            </div>
            <div className="text-[11px] text-text-muted mt-1">
              {tr('智能汇聚收敛率', 'Aggregation Rate')}
            </div>
          </Card>
        </div>

        {/* Engineer Load Distribution */}
        {analytics?.engineer_loads && analytics.engineer_loads.length > 0 && (
          <Card className="!p-0 overflow-hidden">
            <div className="p-4 border-b border-border">
              <span className="text-xs font-semibold text-text uppercase tracking-wider">
                {tr('工程师值班负荷分布', 'Engineer Load Distribution')}
              </span>
            </div>
            <div className="overflow-x-auto">
              <table className="w-full text-left text-xs border-collapse">
                <thead>
                  <tr className="border-b border-border bg-bg/50 text-text-muted font-semibold">
                    <th className="py-2.5 px-4">{tr('工程师', 'Engineer')}</th>
                    <th className="py-2.5 px-4">{tr('值班时长', 'On-Call Hours')}</th>
                    <th className="py-2.5 px-4">{tr('处置告警', 'Incidents Handled')}</th>
                    <th className="py-2.5 px-4">{tr('夜间电话', 'Night Calls')}</th>
                    <th className="py-2.5 px-4">{tr('疲劳评分', 'Fatigue Score')}</th>
                    <th className="py-2.5 px-4">{tr('健康状态', 'Health Status')}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {analytics.engineer_loads.map((eng) => (
                    <tr key={eng.user_id} className="hover:bg-bg/40 transition-colors">
                      <td className="py-2.5 px-4 font-semibold text-text">{eng.name}</td>
                      <td className="py-2.5 px-4 font-mono">{eng.on_call_hours}h</td>
                      <td className="py-2.5 px-4 font-mono">{eng.incidents_handled}</td>
                      <td className="py-2.5 px-4 font-mono text-amber-600 dark:text-amber-400">
                        {eng.night_calls}
                      </td>
                      <td className="py-2.5 px-4 font-mono font-bold text-indigo-600 dark:text-indigo-400">
                        {eng.fatigue_score}
                      </td>
                      <td className="py-2.5 px-4">
                        <Chip
                          tone={
                            eng.health_status === 'healthy'
                              ? 'success'
                              : eng.health_status === 'moderate'
                              ? 'warning'
                              : 'danger'
                          }
                          dense
                        >
                          {eng.health_status === 'healthy'
                            ? tr('健康', 'Healthy')
                            : eng.health_status === 'moderate'
                            ? tr('适中', 'Moderate')
                            : tr('超载', 'Overloaded')}
                        </Chip>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Card>
        )}
      </section>

      {/* 3. Top Noisy Alerts Governance */}
      <section className="space-y-3">
        <div className="flex items-center gap-2">
          <TrendingDown className="text-amber-500" size={18} />
          <h2 className="text-base font-semibold text-text">
            {tr('高频噪音与抖动告警治理', 'Top Noisy Alerts Governance')}
          </h2>
        </div>

        <Card className="!p-0 overflow-hidden">
          {noisyAlerts.length === 0 ? (
            <div className="py-6 text-center text-xs text-text-muted">
              {tr('暂无高频噪音告警', 'No noisy alerts detected')}
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-left text-xs border-collapse">
                <thead>
                  <tr className="border-b border-border bg-bg/50 text-text-muted font-semibold">
                    <th className="py-2.5 px-4">{tr('告警指纹/名称', 'Alert Name')}</th>
                    <th className="py-2.5 px-4">{tr('所属微服务', 'Service')}</th>
                    <th className="py-2.5 px-4">{tr('等级', 'Severity')}</th>
                    <th className="py-2.5 px-4">{tr('触发频次', 'Trigger Count')}</th>
                    <th className="py-2.5 px-4">{tr('抖动评分', 'Flapping Score')}</th>
                    <th className="py-2.5 px-4">{tr('治理建议', 'Recommended Action')}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {noisyAlerts.map((na) => (
                    <tr key={na.fingerprint} className="hover:bg-bg/40 transition-colors">
                      <td className="py-2.5 px-4 font-mono font-semibold text-text">
                        <div>{na.alert_name}</div>
                        <div className="text-[11px] text-text-faint">{na.fingerprint}</div>
                      </td>
                      <td className="py-2.5 px-4 font-medium text-text">{na.service}</td>
                      <td className="py-2.5 px-4">
                        <Chip tone={na.severity === 'P0' || na.severity === 'P1' ? 'danger' : 'warning'} dense>
                          {na.severity}
                        </Chip>
                      </td>
                      <td className="py-2.5 px-4 font-mono font-bold text-amber-600 dark:text-amber-400">
                        {na.trigger_count} {tr('次', 'times')}
                      </td>
                      <td className="py-2.5 px-4 font-mono">{na.flapping_score}</td>
                      <td className="py-2.5 px-4 text-text-muted max-w-sm truncate" title={na.recommended_action}>
                        {na.recommended_action}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      </section>

      {/* 4. Shift Handoff & Pre-Shift Reminder Logs */}
      <section className="space-y-3">
        <div className="flex items-center gap-2">
          <Bell className="text-indigo-500" size={18} />
          <h2 className="text-base font-semibold text-text">
            {tr('交接签收与接班提醒记录', 'Handoff Sign-Off & Reminder Logs')}
          </h2>
        </div>

        <Card className="!p-0 overflow-hidden">
          {handoffLogs.length === 0 ? (
            <EmptyState
              icon={Bell}
              title={tr('暂无交接签收与提醒记录', 'No Handoff or Reminder Logs Yet')}
              hint={tr(
                '系统将在交班前自动发送班前提醒，也可在上方手动触发提醒或签署交接。',
                'Pre-shift reminders run automatically before shift transitions, or you can trigger a reminder or sign off above.'
              )}
            />
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-left text-xs border-collapse text-text">
                <thead>
                  <tr className="border-b border-border bg-bg/50 text-text-muted font-semibold">
                    <th className="py-2.5 px-4 whitespace-nowrap">{tr('时间', 'Time')}</th>
                    <th className="py-2.5 px-4 whitespace-nowrap">{tr('排班与班次', 'Schedule & Shift')}</th>
                    <th className="py-2.5 px-4 whitespace-nowrap">{tr('记录类型', 'Type')}</th>
                    <th className="py-2.5 px-4 whitespace-nowrap">{tr('值班与交接人', 'Engineers')}</th>
                    <th className="py-2.5 px-4 whitespace-nowrap">{tr('通道', 'Channel')}</th>
                    <th className="py-2.5 px-4 whitespace-nowrap">{tr('状态', 'Status')}</th>
                    <th className="py-2.5 px-4">{tr('交接摘要与详情', 'Summary')}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {handoffLogs.map((item) => {
                    const typeLabel =
                      item.reminder_type === 'shift_handoff'
                        ? tr('交接签署', 'Sign-Off')
                        : item.reminder_type === 'manual_reminder'
                        ? tr('手动接班提醒', 'Manual Reminder')
                        : tr('班前自动提醒', 'Pre-Shift Reminder');
                    const deliveryStatus = item.status || (item.ack_status === 'signed_off' ? 'sent' : 'pending');
                    const statusTone =
                      deliveryStatus === 'failed'
                        ? 'danger'
                        : deliveryStatus === 'sent'
                        ? 'success'
                        : 'warning';
                    const statusLabel =
                      item.ack_status === 'signed_off'
                        ? tr('已签署', 'Signed Off')
                        : deliveryStatus === 'failed'
                        ? tr('投递失败', 'Failed')
                        : deliveryStatus === 'sent'
                        ? tr('已发送', 'Sent')
                        : tr('待处理', 'Pending');

                    return (
                      <tr key={item.id} className="hover:bg-bg/40 transition-colors">
                        <td className="py-2.5 px-4 font-mono text-text-muted whitespace-nowrap">
                          {new Date(item.created_at).toLocaleString()}
                        </td>
                        <td className="py-2.5 px-4 whitespace-nowrap">
                          <div className="font-medium text-text">
                            {item.schedule_name || `#${item.schedule_id}`}
                          </div>
                          <div className="text-[11px] font-mono text-text-muted">
                            {new Date(item.shift_start).toLocaleDateString()} ~{' '}
                            {new Date(item.shift_end).toLocaleDateString()}
                          </div>
                        </td>
                        <td className="py-2.5 px-4 whitespace-nowrap">
                          <Chip tone={item.reminder_type === 'shift_handoff' ? 'accent' : 'info'} dense>
                            {typeLabel}
                          </Chip>
                        </td>
                        <td className="py-2.5 px-4 whitespace-nowrap text-text">
                          <div>{item.user_name || `User#${item.user_id}`}</div>
                          {item.previous_user_name && (
                            <div className="text-[11px] text-text-muted">
                              {tr('交出人：', 'From: ')}
                              {item.previous_user_name}
                            </div>
                          )}
                        </td>
                        <td className="py-2.5 px-4 font-mono text-text-muted whitespace-nowrap">
                          {item.channel_type || '-'}
                        </td>
                        <td className="py-2.5 px-4 whitespace-nowrap">
                          <Chip tone={statusTone} dense>
                            {statusLabel}
                          </Chip>
                        </td>
                        <td className="py-2.5 px-4 text-text min-w-[220px] max-w-md">
                          <div className="break-words">{item.notes || '-'}</div>
                          {item.error_message && (
                            <div className="text-[11px] text-red-600 dark:text-red-400 mt-0.5 break-words">
                              {item.error_message}
                            </div>
                          )}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      </section>
      {dialog}
    </div>
  );
}
