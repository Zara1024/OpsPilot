import { useState, useEffect, useCallback } from 'react';
import {
  FileText,
  Activity,
  AlertTriangle,
  HeartPulse,
  Clock,
  UserCheck,
  CheckCircle2,
  TrendingDown,
  RefreshCw,
  CheckSquare,
} from 'lucide-react';
import {
  Button,
  Card,
  Chip,
  EmptyState,
  Label,
  Textarea,
} from '@/components/ui';
import { useI18n } from '@/i18n/locale';
import {
  getCurrentHandoff,
  ackHandoff,
  getBurnoutAnalytics,
  getNoisyAlerts,
  type CurrentHandoffStatus,
  type BurnoutAnalytics,
  type NoisyAlertItem,
  type OnCallSchedule,
} from '@/api/oncall';

interface HandoffAnalyticsTabProps {
  schedules: OnCallSchedule[];
}

export function HandoffAnalyticsTab({ schedules }: HandoffAnalyticsTabProps) {
  const { tr } = useI18n();

  const [handoff, setHandoff] = useState<CurrentHandoffStatus | null>(null);
  const [analytics, setAnalytics] = useState<BurnoutAnalytics | null>(null);
  const [noisyAlerts, setNoisyAlerts] = useState<NoisyAlertItem[]>([]);
  const [loading, setLoading] = useState(true);

  // Handoff signing
  const [handoffNotes, setHandoffNotes] = useState('');
  const [signingHandoff, setSigningHandoff] = useState(false);
  const [signSuccess, setSignSuccess] = useState(false);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const [hRes, aRes, nRes] = await Promise.all([
        getCurrentHandoff().catch(() => null),
        getBurnoutAnalytics().catch(() => null),
        getNoisyAlerts().catch(() => ({ items: [] })),
      ]);
      setHandoff(hRes);
      if (hRes?.notes) setHandoffNotes(hRes.notes);
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
      setSignSuccess(true);
      setTimeout(() => setSignSuccess(false), 4000);
    } catch (e: any) {
      console.error('Failed to ack handoff:', e);
      alert(e.message || tr('交接确认失败', 'Failed to sign handoff'));
    } finally {
      setSigningHandoff(false);
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
              {tr('值班交接仪式与备忘录 (Shift Handoff Summary)', 'Shift Handoff Summary')}
            </h2>
          </div>
          <Button size="sm" variant="ghost" onClick={fetchData} disabled={loading}>
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
                    <span className="text-text-muted">{tr('交班工程师 (Outgoing)', 'Outgoing')}</span>
                    <div className="font-semibold text-text mt-0.5">{handoff.outgoing_user_name}</div>
                  </div>
                  <span className="text-text-muted">➔</span>
                  <div>
                    <span className="text-text-muted">{tr('接班工程师 (Incoming)', 'Incoming')}</span>
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
                      {handoff.status === 'signed_off' ? tr('已签署 (Signed)', 'Signed') : tr('待交接签署 (Pending)', 'Pending Sign')}
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
                      {tr('交接班检查清单 (Checklist)', 'Handoff Checklist')}
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
                    {tr('交接备忘录与注意事项 (Handoff Notes)', 'Handoff Notes')}
                  </Label>
                  <Textarea
                    rows={4}
                    value={handoffNotes}
                    onChange={(e) => setHandoffNotes(e.target.value)}
                    placeholder={tr('如：夜间清理了 /var/log 磁盘，观察慢 SQL 抖动等...', 'e.g. Cleaned /var/log, monitor slow queries...')}
                    className="w-full text-xs"
                  />
                </div>

                <Button
                  size="sm"
                  variant="primary"
                  onClick={handleSignHandoff}
                  disabled={signingHandoff}
                  className="w-full"
                >
                  <CheckSquare size={14} className="mr-1.5" />
                  {signingHandoff ? tr('签署中...', 'Signing...') : tr('确认签署交接备忘录', 'Sign Handoff')}
                </Button>
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
            {tr('SRE 疲劳度健康大盘 (Burnout Analytics)', 'SRE Burnout Health Analytics')}
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
              {tr('评级：优秀 (Good)', 'Rating: Good')}
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
                {tr('工程师值班负荷分布 (Engineer Load)', 'Engineer Load Distribution')}
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
                            ? tr('健康 (Healthy)', 'Healthy')
                            : eng.health_status === 'moderate'
                            ? tr('适中 (Moderate)', 'Moderate')
                            : tr('超载 (Overloaded)', 'Overloaded')}
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
            {tr('高频噪音与抖动告警治理 (Top Noisy Alerts)', 'Top Noisy Alerts Governance')}
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
                        {na.trigger_count} 次
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
    </div>
  );
}
