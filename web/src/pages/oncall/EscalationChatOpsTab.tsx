import { useState, useEffect, useCallback } from 'react';
import {
  BellRing,
  PhoneCall,
  MessageSquare,
  ShieldAlert,
  Plus,
  Trash2,
  Edit2,
  CheckCircle2,
  VolumeX,
  Zap,
  Flame,
  ArrowRight,
  Send,
  RefreshCw,
  ExternalLink,
} from 'lucide-react';
import {
  Button,
  Card,
  Chip,
  EmptyState,
  Input,
  Label,
  Select,
  Switch,
} from '@/components/ui';
import { Modal } from '@/components/Modal';
import { useI18n } from '@/i18n/locale';
import {
  listEscalationPolicies,
  createEscalationPolicy,
  updateEscalationPolicy,
  deleteEscalationPolicy,
  getMyNotificationRules,
  setMyNotificationRules,
  testNotification,
  listActiveIncidents,
  triggerChatOpsAction,
  type EscalationPolicy,
  type UserNotificationRule,
  type ActiveIncident,
  type OnCallSchedule,
} from '@/api/oncall';

interface EscalationChatOpsTabProps {
  schedules: OnCallSchedule[];
}

export function EscalationChatOpsTab({ schedules }: EscalationChatOpsTabProps) {
  const { tr } = useI18n();

  // Active Incidents state
  const [incidents, setIncidents] = useState<ActiveIncident[]>([]);
  const [incidentsLoading, setIncidentsLoading] = useState(false);
  const [actionMessage, setActionMessage] = useState<string | null>(null);
  const [incidentFilter, setIncidentFilter] = useState<'all' | 'active'>('all');

  // Policies state
  const [policies, setPolicies] = useState<EscalationPolicy[]>([]);
  const [policiesLoading, setPoliciesLoading] = useState(false);
  const [policyModalOpen, setPolicyModalOpen] = useState(false);
  const [editingPolicy, setEditingPolicy] = useState<EscalationPolicy | null>(null);
  const [policyName, setPolicyName] = useState('');
  const [policyStep, setPolicyStep] = useState(1);
  const [policyWait, setPolicyWait] = useState(300);
  const [policyTargetType, setPolicyTargetType] = useState<
    'primary_oncall' | 'secondary_oncall' | 'specific_user' | 'webhook' | 'fallback_lead'
  >('primary_oncall');
  const [policyScheduleId, setPolicyScheduleId] = useState<number>(0);
  const [savingPolicy, setSavingPolicy] = useState(false);

  // Personal rules state
  const [urgencyMode, setUrgencyMode] = useState<'high' | 'low'>('high');
  const [myRules, setMyRules] = useState<UserNotificationRule[]>([]);
  const [savingRules, setSavingRules] = useState(false);
  const [rulesSuccess, setRulesSuccess] = useState(false);

  // Test notification state
  const [testChannel, setTestChannel] = useState<'im_dm' | 'sms' | 'voice_call'>('im_dm');
  const [testingNotification, setTestingNotification] = useState(false);
  const [testResult, setTestResult] = useState<string | null>(null);

  const fetchIncidents = useCallback(async (filter = incidentFilter) => {
    setIncidentsLoading(true);
    try {
      const res = await listActiveIncidents(filter);
      setIncidents(res.items || []);
    } catch (e) {
      console.error('Failed to load incidents:', e);
    } finally {
      setIncidentsLoading(false);
    }
  }, [incidentFilter]);

  const fetchPolicies = useCallback(async () => {
    setPoliciesLoading(true);
    try {
      const res = await listEscalationPolicies();
      setPolicies(res.items || []);
    } catch (e) {
      console.error('Failed to load escalation policies:', e);
    } finally {
      setPoliciesLoading(false);
    }
  }, []);

  const fetchMyRules = useCallback(async (urgency: 'high' | 'low') => {
    try {
      const res = await getMyNotificationRules(urgency);
      if (res.items && res.items.length > 0) {
        setMyRules(res.items);
      } else {
        // default rules if empty
        if (urgency === 'high') {
          setMyRules([
            { urgency: 'high', step_number: 1, delay_minutes: 0, channel: 'im_dm', enabled: true },
            { urgency: 'high', step_number: 2, delay_minutes: 3, channel: 'sms', enabled: true },
            { urgency: 'high', step_number: 3, delay_minutes: 8, channel: 'voice_call', enabled: true },
          ]);
        } else {
          setMyRules([
            { urgency: 'low', step_number: 1, delay_minutes: 0, channel: 'im_dm', enabled: true },
            { urgency: 'low', step_number: 2, delay_minutes: 15, channel: 'email', enabled: true },
          ]);
        }
      }
    } catch (e) {
      console.error('Failed to load notification rules:', e);
    }
  }, []);

  useEffect(() => {
    fetchIncidents();
    fetchPolicies();
  }, [fetchIncidents, fetchPolicies]);

  useEffect(() => {
    fetchMyRules(urgencyMode);
  }, [urgencyMode, fetchMyRules]);

  // ChatOps action trigger
  const handleChatOps = async (
    action: 'ack' | 'silence' | 'escalate' | 'warroom',
    incidentId: string
  ) => {
    try {
      const res = await triggerChatOpsAction(action, incidentId);
      setActionMessage(res.message || tr('操作执行成功', 'Action performed successfully'));
      await fetchIncidents();
      setTimeout(() => setActionMessage(null), 4000);
    } catch (e: any) {
      console.error('ChatOps error:', e);
      alert(e.message || tr('ChatOps 执行失败', 'ChatOps failed'));
    }
  };

  // Policy handlers
  const handleOpenCreatePolicy = () => {
    setEditingPolicy(null);
    setPolicyName('');
    setPolicyStep(policies.length + 1);
    setPolicyWait(300);
    setPolicyTargetType('secondary_oncall');
    setPolicyScheduleId(schedules[0]?.id || 0);
    setPolicyModalOpen(true);
  };

  const handleOpenEditPolicy = (p: EscalationPolicy) => {
    setEditingPolicy(p);
    setPolicyName(p.name);
    setPolicyStep(p.step_number);
    setPolicyWait(p.wait_seconds);
    setPolicyTargetType(p.target_type);
    setPolicyScheduleId(p.schedule_id);
    setPolicyModalOpen(true);
  };

  const handleSavePolicy = async () => {
    if (!policyName.trim() || !policyScheduleId) return;
    setSavingPolicy(true);
    try {
      const data = {
        schedule_id: Number(policyScheduleId),
        name: policyName.trim(),
        step_number: Number(policyStep),
        wait_seconds: Number(policyWait),
        target_type: policyTargetType,
      };
      if (editingPolicy && editingPolicy.id) {
        await updateEscalationPolicy(editingPolicy.id, data);
      } else {
        await createEscalationPolicy(data);
      }
      setPolicyModalOpen(false);
      await fetchPolicies();
    } catch (e) {
      console.error('Failed to save policy:', e);
    } finally {
      setSavingPolicy(false);
    }
  };

  const handleDeletePolicy = async (id?: number) => {
    if (!id) return;
    if (!window.confirm(tr('确定要删除该升级策略步骤吗？', 'Are you sure you want to delete this step?'))) {
      return;
    }
    try {
      await deleteEscalationPolicy(id);
      await fetchPolicies();
    } catch (e) {
      console.error('Failed to delete policy:', e);
    }
  };

  // Personal rules save
  const handleSaveRules = async () => {
    setSavingRules(true);
    setRulesSuccess(false);
    try {
      await setMyNotificationRules(urgencyMode, myRules);
      setRulesSuccess(true);
      setTimeout(() => setRulesSuccess(false), 3000);
    } catch (e) {
      console.error('Failed to save rules:', e);
    } finally {
      setSavingRules(false);
    }
  };

  // Test channel trigger
  const handleTestNotification = async () => {
    setTestingNotification(true);
    setTestResult(null);
    try {
      const res = await testNotification(testChannel, urgencyMode);
      setTestResult(
        `${tr('测试发送成功', 'Test notification sent')}: ${res.target || ''} [${res.message || 'OK'}]`
      );
    } catch (e: any) {
      setTestResult(`${tr('测试失败', 'Failed')}: ${e.message}`);
    } finally {
      setTestingNotification(false);
    }
  };

  const getTargetTypeLabel = (type: string) => {
    switch (type) {
      case 'primary_oncall':
        return tr('当前一线主值班人 (Primary)', 'Primary On-Call');
      case 'secondary_oncall':
        return tr('二线技术专家 (Secondary Support)', 'Secondary Support');
      case 'fallback_lead':
        return tr('SRE 应急负责人 (Fallback Lead)', 'SRE Lead Fallback');
      case 'webhook':
        return tr('IM 群机器人 Webhook 广播', 'IM Group Webhook Broadcast');
      default:
        return tr('指定人员', 'Specific User');
    }
  };

  return (
    <div className="space-y-8">
      {/* 1. Active Incidents ChatOps Hub */}
      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Flame className="text-red-500" size={18} />
            <h2 className="text-base font-semibold text-text">
              {tr('活跃故障协同台 (Incident ChatOps Hub)', 'Incident ChatOps Hub')}
            </h2>
            <Chip tone="danger" dense>
              {incidents.filter((i) => i.status !== 'resolved').length} {tr('起未解决', 'active')}
            </Chip>
          </div>
          <div className="flex items-center gap-2">
            <div className="inline-flex rounded-lg border border-border p-0.5 text-xs bg-bg">
              <button
                type="button"
                onClick={() => {
                  setIncidentFilter('all');
                  fetchIncidents('all');
                }}
                className={`px-2.5 py-1 rounded font-medium transition-colors ${
                  incidentFilter === 'all'
                    ? 'bg-card text-indigo-600 dark:text-indigo-400 shadow-sm'
                    : 'text-text-muted hover:text-text'
                }`}
              >
                {tr('全部记录', 'All')}
              </button>
              <button
                type="button"
                onClick={() => {
                  setIncidentFilter('active');
                  fetchIncidents('active');
                }}
                className={`px-2.5 py-1 rounded font-medium transition-colors ${
                  incidentFilter === 'active'
                    ? 'bg-card text-indigo-600 dark:text-indigo-400 shadow-sm'
                    : 'text-text-muted hover:text-text'
                }`}
              >
                {tr('仅活跃告警', 'Active Only')}
              </button>
            </div>
            <Button size="sm" variant="ghost" onClick={() => fetchIncidents(incidentFilter)} disabled={incidentsLoading}>
              <RefreshCw size={14} className={incidentsLoading ? 'animate-spin' : ''} />
            </Button>
          </div>
        </div>

        {actionMessage && (
          <div className="rounded-lg bg-emerald-500/10 border border-emerald-500/30 px-3 py-2 text-xs text-emerald-600 dark:text-emerald-400">
            {actionMessage}
          </div>
        )}

        <Card className="!p-0 overflow-hidden">
          {incidents.length === 0 ? (
            <div className="py-8 text-center text-xs text-text-muted">
              {tr('当前暂无活跃告警事件，生产系统运行平稳。', 'No active incidents. Production system healthy.')}
            </div>
          ) : (
            <div className="divide-y divide-border">
              {incidents.map((inc) => (
                <div key={inc.id} className="p-4 flex flex-wrap items-center justify-between gap-4">
                  <div className="space-y-1">
                    <div className="flex items-center gap-2">
                      <Chip
                        tone={
                          inc.severity === 'P0' || inc.severity === 'P1'
                            ? 'danger'
                            : 'warning'
                        }
                        dense
                      >
                        {inc.severity}
                      </Chip>
                      <span className="font-semibold text-sm text-text">{inc.title}</span>
                      <span className="font-mono text-xs text-text-muted">[{inc.id}]</span>
                    </div>
                    <div className="text-xs text-text-muted flex items-center gap-3">
                      <span>
                        {tr('所属排班：', 'Schedule: ')}
                        <strong className="text-text">{inc.schedule_name}</strong>
                      </span>
                      <span>
                        {tr('当前值班责任人：', 'Assignee: ')}
                        <strong className="text-indigo-600 dark:text-indigo-400">
                          {inc.current_assignee}
                        </strong>
                      </span>
                      <span>
                        {tr('状态：', 'Status: ')}
                        <Chip
                          tone={
                            inc.status === 'firing'
                              ? 'danger'
                              : inc.status === 'acknowledged'
                              ? 'success'
                              : 'default'
                          }
                          dense
                        >
                          {inc.status === 'firing'
                            ? tr('触发中 (Firing)', 'Firing')
                            : inc.status === 'acknowledged'
                            ? tr('已认领 (Acked)', 'Acknowledged')
                            : inc.status === 'resolved'
                            ? tr('已恢复 (Resolved)', 'Resolved')
                            : inc.status}
                        </Chip>
                      </span>
                      {inc.acked_by && (
                        <span>
                          ({tr('由 ', 'by ')}{inc.acked_by} {tr('认领', 'acked')})
                        </span>
                      )}
                    </div>
                  </div>

                  {/* ChatOps Action Buttons */}
                  <div className="flex items-center gap-2">
                    {inc.status !== 'acknowledged' && inc.status !== 'resolved' && (
                      <Button
                        size="sm"
                        variant="primary"
                        onClick={() => handleChatOps('ack', inc.id)}
                      >
                        <CheckCircle2 size={14} className="mr-1.5" />
                        {tr('认领 (Ack)', 'Acknowledge')}
                      </Button>
                    )}
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => handleChatOps('silence', inc.id)}
                    >
                      <VolumeX size={14} className="mr-1.5" />
                      {tr('静音 30m', 'Silence 30m')}
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => handleChatOps('escalate', inc.id)}
                    >
                      <Zap size={14} className="mr-1.5 text-amber-500" />
                      {tr('立即升级', 'Escalate')}
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      className="text-indigo-600 dark:text-indigo-400"
                      onClick={() => handleChatOps('warroom', inc.id)}
                    >
                      <ExternalLink size={14} className="mr-1.5" />
                      {tr('应急作战室', 'War Room')}
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </Card>
      </section>

      {/* 2. Team Escalation Policies Pipeline */}
      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-base font-semibold text-text">
              {tr('团队多级超时升级流水线 (Escalation Policies)', 'Team Escalation Policies')}
            </h2>
            <p className="text-xs text-text-muted mt-0.5">
              {tr(
                '未认领告警按设有时限自动沿梯队递进呼叫，确保任何紧急事故 100% 闭环响应。',
                'Unacknowledged alerts automatically escalate through tiers until resolved.'
              )}
            </p>
          </div>
          <Button size="sm" variant="primary" onClick={handleOpenCreatePolicy}>
            <Plus size={14} className="mr-1.5" />
            {tr('添加升级步骤', 'Add Step')}
          </Button>
        </div>

        <Card className="p-4">
          {policiesLoading ? (
            <div className="flex h-32 items-center justify-center">
              <RefreshCw size={18} className="animate-spin text-indigo-500" />
            </div>
          ) : policies.length === 0 ? (
            <div className="py-6 text-center text-xs text-text-faint">
              {tr('尚未配置升级策略', 'No escalation policies configured')}
            </div>
          ) : (
            <div className="flex flex-col md:flex-row items-stretch md:items-center gap-3 overflow-x-auto py-2">
              {policies.map((p, idx) => (
                <div key={p.id || idx} className="flex items-center gap-3 shrink-0">
                  <div className="flex flex-col justify-between rounded-lg border border-border bg-bg/60 p-4 w-64 h-36">
                    <div className="space-y-1">
                      <div className="flex items-center justify-between">
                        <span className="inline-flex h-5 w-5 items-center justify-center rounded-full bg-indigo-500/10 text-indigo-600 dark:text-indigo-400 font-bold text-xs">
                          {p.step_number}
                        </span>
                        <div className="flex items-center gap-1">
                          <button
                            onClick={() => handleOpenEditPolicy(p)}
                            className="text-text-muted hover:text-text p-1"
                          >
                            <Edit2 size={12} />
                          </button>
                          <button
                            onClick={() => handleDeletePolicy(p.id)}
                            className="text-red-500 hover:text-red-600 p-1"
                          >
                            <Trash2 size={12} />
                          </button>
                        </div>
                      </div>
                      <div className="font-semibold text-xs text-text truncate mt-1">{p.name}</div>
                      <div className="text-[11px] text-text-muted">
                        {p.wait_seconds === 0
                          ? tr('即刻响应 (0 秒)', 'Immediate (0s)')
                          : tr(`${Math.round(p.wait_seconds / 60)} 分钟未认领后升级`, `Wait ${Math.round(p.wait_seconds / 60)}m`)}
                      </div>
                    </div>
                    <div className="pt-2 border-t border-border">
                      <span className="text-[11px] font-medium text-indigo-600 dark:text-indigo-400 block truncate">
                        {getTargetTypeLabel(p.target_type)}
                      </span>
                    </div>
                  </div>
                  {idx < policies.length - 1 && (
                    <ArrowRight size={18} className="text-text-muted shrink-0 hidden md:block" />
                  )}
                </div>
              ))}
            </div>
          )}
        </Card>
      </section>

      {/* 3. Personal Notification Ladder & Connectivity Test */}
      <section className="space-y-4">
        <div>
          <h2 className="text-base font-semibold text-text">
            {tr('个人多通道通知阶梯与偏好 (Personal Notification Profile)', 'Personal Notification Ladder')}
          </h2>
          <p className="text-xs text-text-muted mt-0.5">
            {tr(
              '工程师可按紧急程度个性化配置私聊、短信及语音电话阶梯，并在线测试触达通路。',
              'Configure custom personal notification progression rules across IM, SMS, and Voice Calls.'
            )}
          </p>
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
          {/* Rules Configuration */}
          <Card className="p-4 lg:col-span-2 space-y-4">
            <div className="flex items-center justify-between border-b border-border pb-3">
              <div className="flex items-center gap-2">
                <Button
                  size="sm"
                  variant={urgencyMode === 'high' ? 'primary' : 'ghost'}
                  onClick={() => setUrgencyMode('high')}
                >
                  {tr('高优先级 (P0 / P1 强触达)', 'High Urgency (P0/P1)')}
                </Button>
                <Button
                  size="sm"
                  variant={urgencyMode === 'low' ? 'primary' : 'ghost'}
                  onClick={() => setUrgencyMode('low')}
                >
                  {tr('低优先级 (P2 / P3 静默)', 'Low Urgency (P2/P3)')}
                </Button>
              </div>
              <Button
                size="sm"
                variant="primary"
                onClick={handleSaveRules}
                disabled={savingRules}
              >
                {savingRules ? tr('保存中...', 'Saving...') : tr('保存阶梯偏好', 'Save Profile')}
              </Button>
            </div>

            {rulesSuccess && (
              <div className="rounded bg-emerald-500/10 border border-emerald-500/30 p-2 text-xs text-emerald-600 dark:text-emerald-400">
                {tr('个人通知偏好保存成功！', 'Notification ladder saved successfully!')}
              </div>
            )}

            <div className="space-y-3">
              {myRules.map((rule, idx) => (
                <div
                  key={idx}
                  className="flex items-center justify-between rounded-lg border border-border bg-bg/50 p-3 text-xs"
                >
                  <div className="flex items-center gap-3">
                    <span className="flex h-6 w-6 items-center justify-center rounded-full bg-indigo-500/10 text-indigo-600 font-bold">
                      {rule.step_number}
                    </span>
                    <div>
                      <div className="font-semibold text-text">
                        {rule.delay_minutes === 0
                          ? tr('第 1 级：即刻通知 (0 分钟)', 'Step 1: Immediate (0m)')
                          : tr(
                              `第 ${rule.step_number} 级：超时 ${rule.delay_minutes} 分钟未认领`,
                              `Step ${rule.step_number}: After ${rule.delay_minutes}m unacked`
                            )}
                      </div>
                      <div className="text-text-muted mt-0.5">
                        {tr('通知媒介：', 'Channel: ')}
                        <span className="font-medium text-text uppercase">{rule.channel}</span>
                      </div>
                    </div>
                  </div>

                  <div className="flex items-center gap-4">
                    <Select
                      value={rule.channel}
                      onValueChange={(val) => {
                        const next = [...myRules];
                        next[idx].channel = val as any;
                        setMyRules(next);
                      }}
                      options={[
                        { value: 'im_dm', label: tr('IM 私聊强提醒', 'IM Direct Message') },
                        { value: 'im_group_at', label: tr('IM 群聊 @ 提醒', 'IM Group Mention') },
                        { value: 'sms', label: tr('加急短信', 'Urgent SMS') },
                        { value: 'voice_call', label: tr('语音电话呼叫', 'Voice Call') },
                        { value: 'email', label: tr('邮件通知', 'Email') },
                      ]}
                      className="w-36 text-xs"
                    />

                    <Switch
                      checked={rule.enabled}
                      onCheckedChange={(checked) => {
                        const next = [...myRules];
                        next[idx].enabled = checked;
                        setMyRules(next);
                      }}
                    />
                  </div>
                </div>
              ))}
            </div>
          </Card>

          {/* Channel Connectivity Test Card */}
          <Card className="p-4 space-y-4">
            <div className="flex items-center gap-2 border-b border-border pb-3">
              <Send size={16} className="text-indigo-500" />
              <h3 className="text-xs font-semibold text-text uppercase tracking-wider">
                {tr('多通道连通性实测', 'Channel Test Simulator')}
              </h3>
            </div>

            <p className="text-xs text-text-muted">
              {tr(
                '向当前登录用户的手机号与 IM 账号推送一条模拟测试信号，验证外部网关触达能力。',
                'Send a test notification to your registered channels to verify delivery.'
              )}
            </p>

            <div className="space-y-3">
              <div>
                <Label className="text-xs">{tr('选择测试通道', 'Select Channel')}</Label>
                <Select
                  value={testChannel}
                  onValueChange={(val) => setTestChannel(val as any)}
                  options={[
                    { value: 'im_dm', label: tr('企业微信/飞书/钉钉私聊', 'IM Bot DM') },
                    { value: 'sms', label: tr('短信服务网关 (SMS)', 'SMS Gateway') },
                    { value: 'voice_call', label: tr('语音电话外呼 (Voice Call)', 'Voice Call') },
                  ]}
                  className="mt-1 w-full text-xs"
                />
              </div>

              <Button
                size="sm"
                variant="primary"
                onClick={handleTestNotification}
                disabled={testingNotification}
                className="w-full"
              >
                <Send size={14} className="mr-1.5" />
                {testingNotification ? tr('正在发起测试...', 'Sending...') : tr('发起通道连通性测试', 'Trigger Channel Test')}
              </Button>

              {testResult && (
                <div className="rounded border border-border bg-bg p-2.5 text-xs font-mono text-text break-all">
                  {testResult}
                </div>
              )}
            </div>
          </Card>
        </div>
      </section>

      {/* Policy Modal */}
      <Modal
        open={policyModalOpen}
        onClose={() => setPolicyModalOpen(false)}
        title={editingPolicy ? tr('编辑升级步骤', 'Edit Escalation Step') : tr('添加升级步骤', 'Add Escalation Step')}
        size="sm"
        footer={
          <div className="flex justify-end gap-2 w-full">
            <Button size="sm" variant="ghost" onClick={() => setPolicyModalOpen(false)}>
              {tr('取消', 'Cancel')}
            </Button>
            <Button
              size="sm"
              variant="primary"
              onClick={handleSavePolicy}
              disabled={savingPolicy || !policyName.trim()}
            >
              {savingPolicy ? tr('保存中...', 'Saving...') : tr('确认保存', 'Save')}
            </Button>
          </div>
        }
      >
        <div className="space-y-3 py-2 text-xs">
          <div>
            <Label className="text-xs">{tr('步骤描述', 'Step Name')} *</Label>
            <Input
              value={policyName}
              onChange={(e) => setPolicyName(e.target.value)}
              placeholder="e.g. 5分钟未认领自动升级至二线专家 (短信+电话)"
              className="mt-1 text-xs"
            />
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div>
              <Label className="text-xs">{tr('步骤次序', 'Step Order')}</Label>
              <Input
                type="number"
                value={policyStep}
                onChange={(e) => setPolicyStep(Number(e.target.value))}
                className="mt-1 text-xs"
              />
            </div>
            <div>
              <Label className="text-xs">{tr('超时等待 (秒)', 'Wait Seconds')}</Label>
              <Input
                type="number"
                value={policyWait}
                onChange={(e) => setPolicyWait(Number(e.target.value))}
                className="mt-1 text-xs"
              />
            </div>
          </div>

          <div>
            <Label className="text-xs">{tr('目标对象类型', 'Target Type')}</Label>
            <Select
              value={policyTargetType}
              onValueChange={(val) => setPolicyTargetType(val as any)}
              options={[
                { value: 'primary_oncall', label: tr('一线在岗主值班人 (Primary)', 'Primary On-Call') },
                { value: 'secondary_oncall', label: tr('二线技术专家 (Secondary)', 'Secondary Support') },
                { value: 'fallback_lead', label: tr('SRE 应急负责人 (Fallback Lead)', 'SRE Lead') },
                { value: 'webhook', label: tr('IM 群机器人广播 (Webhook)', 'Webhook') },
              ]}
              className="mt-1 w-full text-xs"
            />
          </div>

          <div>
            <Label className="text-xs">{tr('所属排班计划', 'Schedule')}</Label>
            <Select
              value={String(policyScheduleId)}
              onValueChange={(val) => setPolicyScheduleId(Number(val))}
              options={schedules.map((s) => ({ value: String(s.id), label: s.name }))}
              className="mt-1 w-full text-xs"
            />
          </div>
        </div>
      </Modal>
    </div>
  );
}
