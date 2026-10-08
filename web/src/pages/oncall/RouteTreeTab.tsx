import { useState, useEffect, useCallback } from 'react';
import {
  GitFork,
  Plus,
  Play,
  Trash2,
  Edit2,
  AlertCircle,
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
  Switch,
  Textarea,
} from '@/components/ui';
import { Modal } from '@/components/Modal';
import { useI18n } from '@/i18n/locale';
import {
  listRoutes,
  createRoute,
  updateRoute,
  deleteRoute,
  testMatchRoute,
  type OnCallRoute,
  type OnCallSchedule,
  type EscalationPolicy,
  type LabelMatcher,
  type RouteMatchResult,
} from '@/api/oncall';

interface RouteTreeTabProps {
  schedules: OnCallSchedule[];
  escalationPolicies: EscalationPolicy[];
}

export function RouteTreeTab({ schedules, escalationPolicies }: RouteTreeTabProps) {
  const { tr } = useI18n();

  const [routes, setRoutes] = useState<OnCallRoute[]>([]);
  const [loading, setLoading] = useState(true);

  // Simulator state
  const [simulatorOpen, setSimulatorOpen] = useState(false);
  const [testPayload, setTestPayload] = useState(
    JSON.stringify({ service: 'payment', severity: 'critical', environment: 'production' }, null, 2)
  );
  const [testingMatch, setTestingMatch] = useState(false);
  const [matchResult, setMatchResult] = useState<RouteMatchResult | null>(null);
  const [matchError, setMatchError] = useState<string | null>(null);

  // Modal create/edit state
  const [modalOpen, setModalOpen] = useState(false);
  const [editingRoute, setEditingRoute] = useState<OnCallRoute | null>(null);
  const [name, setName] = useState('');
  const [priority, setPriority] = useState<number>(10);
  const [targetScheduleId, setTargetScheduleId] = useState<number>(0);
  const [escalationPolicyId, setEscalationPolicyId] = useState<number>(0);
  const [groupWait, setGroupWait] = useState<number>(30);
  const [groupInterval, setGroupInterval] = useState<number>(300);
  const [matchers, setMatchers] = useState<LabelMatcher[]>([
    { name: 'service', operator: '=', value: 'payment' },
  ]);
  const [saving, setSaving] = useState(false);

  const fetchRoutes = useCallback(async () => {
    setLoading(true);
    try {
      const res = await listRoutes();
      setRoutes(res.items || []);
    } catch (e) {
      console.error('Failed to load routes:', e);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchRoutes();
  }, [fetchRoutes]);

  const handleOpenCreate = () => {
    setEditingRoute(null);
    setName('');
    setPriority(10);
    setTargetScheduleId(schedules[0]?.id || 0);
    setEscalationPolicyId(escalationPolicies[0]?.id || 0);
    setGroupWait(30);
    setGroupInterval(300);
    setMatchers([{ name: 'severity', operator: '=', value: 'critical' }]);
    setModalOpen(true);
  };

  const handleOpenEdit = (route: OnCallRoute) => {
    setEditingRoute(route);
    setName(route.name);
    setPriority(route.priority);
    setTargetScheduleId(route.schedule_id);
    setEscalationPolicyId(route.escalation_policy_id || 0);
    setGroupWait(route.group_wait_seconds || 30);
    setGroupInterval(route.group_interval_seconds || 300);

    let parsedMatchers: LabelMatcher[] = [];
    if (route.matchers && route.matchers.length > 0) {
      parsedMatchers = route.matchers;
    } else if (route.matcher_json) {
      try {
        parsedMatchers = JSON.parse(route.matcher_json);
      } catch {
        parsedMatchers = [];
      }
    }
    setMatchers(parsedMatchers.length > 0 ? parsedMatchers : [{ name: '', operator: '=', value: '' }]);
    setModalOpen(true);
  };

  const handleSaveRoute = async () => {
    if (!name.trim() || !targetScheduleId) return;
    setSaving(true);
    try {
      const validMatchers = matchers.filter((m) => m.name.trim() !== '');
      const payload: Partial<OnCallRoute> & { matchers?: LabelMatcher[] } = {
        name: name.trim(),
        priority: Number(priority),
        schedule_id: Number(targetScheduleId),
        escalation_policy_id: Number(escalationPolicyId) || undefined,
        group_wait_seconds: Number(groupWait),
        group_interval_seconds: Number(groupInterval),
        matchers: validMatchers,
        matcher_json: JSON.stringify(validMatchers),
      };

      if (editingRoute) {
        await updateRoute(editingRoute.id, payload);
      } else {
        await createRoute(payload);
      }
      setModalOpen(false);
      await fetchRoutes();
    } catch (e) {
      console.error('Failed to save route:', e);
    } finally {
      setSaving(false);
    }
  };

  const handleToggleEnabled = async (route: OnCallRoute) => {
    try {
      await updateRoute(route.id, { enabled: !route.enabled });
      setRoutes((prev) =>
        prev.map((r) => (r.id === route.id ? { ...r, enabled: !r.enabled } : r))
      );
    } catch (e) {
      console.error('Failed to toggle route status:', e);
    }
  };

  const handleDelete = async (id: number) => {
    if (!window.confirm(tr('确定要删除该告警路由规则吗？', 'Are you sure you want to delete this route?'))) {
      return;
    }
    try {
      await deleteRoute(id);
      await fetchRoutes();
    } catch (e) {
      console.error('Failed to delete route:', e);
    }
  };

  const handleRunMatch = async () => {
    setTestingMatch(true);
    setMatchError(null);
    setMatchResult(null);
    try {
      const parsed = JSON.parse(testPayload);
      const res = await testMatchRoute(parsed);
      setMatchResult(res);
    } catch (e: any) {
      setMatchError(e.message || tr('JSON 解析失败或匹配出错', 'Invalid JSON or matching error'));
    } finally {
      setTestingMatch(false);
    }
  };

  const getScheduleName = (id: number) => {
    return schedules.find((s) => s.id === id)?.name || `Schedule #${id}`;
  };

  const getPolicyName = (id?: number) => {
    if (!id) return tr('默认直发', 'Direct Delivery');
    return escalationPolicies.find((p) => p.id === id)?.name || `Policy #${id}`;
  };

  const renderMatchers = (route: OnCallRoute) => {
    let list: LabelMatcher[] = [];
    if (route.matchers && route.matchers.length > 0) {
      list = route.matchers;
    } else if (route.matcher_json) {
      try {
        list = JSON.parse(route.matcher_json);
      } catch {
        list = [];
      }
    }
    if (list.length === 0) {
      return <span className="text-xs text-text-faint">{tr('无匹配条件 (全匹配)', 'Match All')}</span>;
    }
    return (
      <div className="flex flex-wrap gap-1.5">
        {list.map((m, idx) => (
          <span
            key={idx}
            className="inline-flex items-center gap-1 rounded bg-bg px-2 py-0.5 text-[11px] font-mono border border-border text-text"
          >
            <span className="font-semibold text-indigo-600 dark:text-indigo-400">{m.name}</span>
            <span className="text-text-muted">{m.operator}</span>
            <span className="text-emerald-600 dark:text-emerald-400">{m.value}</span>
          </span>
        ))}
      </div>
    );
  };

  return (
    <div className="space-y-6">
      {/* Top Action Bar */}
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="min-w-0 flex-1">
          <h2 className="text-base font-semibold text-text">
            {tr('告警多维标签路由树 (Route Tree)', 'Alert Label-Based Route Tree')}
          </h2>
          <p className="text-xs text-text-muted mt-0.5">
            {tr(
              '根据告警事件标签进行精准多叉路由，将故障按业务线与集群分发至目标排班与升级流水线。',
              'Route alert events precisely to targeted schedules and escalation policies based on label matchers.'
            )}
          </p>
        </div>

        <div className="flex shrink-0 items-center gap-2 ml-auto">
          <Button
            size="sm"
            variant="outline"
            onClick={() => setSimulatorOpen(!simulatorOpen)}
          >
            <Play size={14} className="mr-1.5 text-indigo-500" />
            {simulatorOpen ? tr('隐藏模拟器', 'Hide Simulator') : tr('在线匹配模拟器', 'Match Simulator')}
          </Button>
          <Button size="sm" variant="primary" onClick={handleOpenCreate}>
            <Plus size={14} className="mr-1.5" />
            {tr('新建路由规则', 'New Route Rule')}
          </Button>
          <Button
            size="sm"
            variant="outline"
            onClick={fetchRoutes}
            disabled={loading}
            aria-label={tr('刷新', 'Refresh')}
            title={tr('刷新', 'Refresh')}
          >
            <RefreshCw size={14} className={loading ? 'animate-spin' : ''} />
          </Button>
        </div>
      </div>

      {/* Online Match Simulator Card */}
      {simulatorOpen && (
        <Card className="p-4 border-indigo-500/30 bg-indigo-50/20 dark:bg-indigo-950/20">
          <div className="flex items-center justify-between border-b border-border pb-3 mb-3">
            <div className="flex items-center gap-2">
              <Play size={16} className="text-indigo-600 dark:text-indigo-400" />
              <span className="text-sm font-semibold text-text">
                {tr('在线匹配模拟器 (Test Matcher)', 'Online Label Match Simulator')}
              </span>
            </div>
            <span className="text-xs text-text-muted">
              {tr('输入测试告警标签 JSON，即时检验路由命中逻辑', 'Input alert labels JSON to verify routing logic')}
            </span>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div>
              <Label className="text-xs mb-1.5 block">
                {tr('测试告警 Labels JSON Payload', 'Test Alert Labels JSON Payload')}
              </Label>
              <Textarea
                rows={5}
                value={testPayload}
                onChange={(e) => setTestPayload(e.target.value)}
                className="font-mono text-xs w-full"
                placeholder='{"service": "payment", "severity": "critical"}'
              />
              <div className="mt-2 flex justify-end">
                <Button
                  size="sm"
                  variant="primary"
                  onClick={handleRunMatch}
                  disabled={testingMatch}
                >
                  <Play size={14} className="mr-1.5" />
                  {testingMatch ? tr('匹配中...', 'Testing...') : tr('执行匹配测试', 'Run Match Test')}
                </Button>
              </div>
            </div>

            <div className="flex flex-col justify-between rounded-lg border border-border bg-bg p-3.5">
              <div>
                <span className="text-xs font-semibold text-text uppercase tracking-wider block mb-2">
                  {tr('匹配仿真结果', 'Simulation Result')}
                </span>
                {matchError && (
                  <div className="flex items-center gap-2 rounded bg-red-500/10 border border-red-500/30 p-2.5 text-xs text-red-600 dark:text-red-400">
                    <AlertCircle size={16} />
                    <span>{matchError}</span>
                  </div>
                )}
                {matchResult && (
                  <div className="space-y-2 text-xs">
                    <div className="flex items-center justify-between">
                      <span className="text-text-muted">{tr('命中规则', 'Matched Route')}:</span>
                      <span className="font-semibold text-indigo-600 dark:text-indigo-400">
                        {matchResult.matched_route?.name || tr('默认兜底', 'Default Fallback')}
                      </span>
                    </div>
                    <div className="flex items-center justify-between">
                      <span className="text-text-muted">{tr('目标排班计划', 'Target Schedule')}:</span>
                      <span className="font-medium text-text">{matchResult.schedule_name}</span>
                    </div>
                    <div className="flex items-center justify-between">
                      <span className="text-text-muted">{tr('路由类型', 'Route Type')}:</span>
                      <Chip tone={matchResult.is_default ? 'warning' : 'success'} dense>
                        {matchResult.is_default ? tr('默认兜底 (Fallback)', 'Default Fallback') : tr('精准命中 (Matched)', 'Direct Hit')}
                      </Chip>
                    </div>
                  </div>
                )}
                {!matchResult && !matchError && (
                  <div className="text-xs text-text-faint py-6 text-center">
                    {tr('点击左侧【执行匹配测试】查看结果', 'Click "Run Match Test" to view results')}
                  </div>
                )}
              </div>
            </div>
          </div>
        </Card>
      )}

      {/* Route List Card Table */}
      <Card className="!p-0 overflow-hidden">
        {loading ? (
          <div className="flex h-64 items-center justify-center">
            <RefreshCw size={20} className="animate-spin text-indigo-500" />
          </div>
        ) : routes.length === 0 ? (
          <EmptyState
            icon={GitFork}
            title={tr('尚未配置告警路由规则', 'No Route Rules Configured')}
            hint={tr(
              '创建路由规则将微服务及基础设施告警精准导流至对应排班。',
              'Create route rules to distribute infrastructure and service alerts.'
            )}
            action={
              <Button size="sm" variant="primary" onClick={handleOpenCreate}>
                <Plus size={14} className="mr-1.5" />
                {tr('新建路由规则', 'New Route Rule')}
              </Button>
            }
          />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs border-collapse">
              <thead>
                <tr className="border-b border-border bg-bg/50 text-text-muted font-semibold">
                  <th className="py-3 px-4 w-16">{tr('优先级', 'Priority')}</th>
                  <th className="py-3 px-4 w-48">{tr('规则名称', 'Rule Name')}</th>
                  <th className="py-3 px-4">{tr('标签匹配器 (Matchers)', 'Label Matchers')}</th>
                  <th className="py-3 px-4 w-44">{tr('目标排班计划', 'Target Schedule')}</th>
                  <th className="py-3 px-4 w-40">{tr('升级策略', 'Escalation Policy')}</th>
                  <th className="py-3 px-4 w-28 text-center">{tr('状态', 'Status')}</th>
                  <th className="py-3 px-4 w-28 text-left">{tr('操作', 'Actions')}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {routes.map((route) => (
                  <tr key={route.id} className="hover:bg-bg/40 transition-colors">
                    <td className="py-3 px-4">
                      <span className="inline-flex h-6 w-6 items-center justify-center rounded-full bg-indigo-500/10 text-indigo-600 dark:text-indigo-400 font-mono font-bold text-xs">
                        {route.priority}
                      </span>
                    </td>
                    <td className="py-3 px-4 font-semibold text-text">
                      <div>{route.name}</div>
                      <div className="text-[11px] text-text-faint font-mono">ID: {route.id}</div>
                    </td>
                    <td className="py-3 px-4">{renderMatchers(route)}</td>
                    <td className="py-3 px-4 text-text font-medium">
                      {getScheduleName(route.schedule_id)}
                    </td>
                    <td className="py-3 px-4 text-text-muted">
                      {getPolicyName(route.escalation_policy_id)}
                    </td>
                    <td className="py-3 px-4 text-center">
                      <div className="flex items-center justify-center gap-2">
                        <Switch
                          checked={route.enabled}
                          onCheckedChange={() => handleToggleEnabled(route)}
                        />
                        <span className="text-[11px] text-text-muted">
                          {route.enabled ? tr('启用', 'Enabled') : tr('停用', 'Disabled')}
                        </span>
                      </div>
                    </td>
                    <td className="py-3 px-4 text-left">
                      <div className="flex items-center gap-1.5">
                        <Button
                          size="sm"
                          variant="ghost"
                          title={tr('编辑', 'Edit')}
                          aria-label={tr('编辑', 'Edit')}
                          onClick={() => handleOpenEdit(route)}
                          className="h-7 w-7 p-0"
                        >
                          <Edit2 size={13} className="text-text-muted" />
                        </Button>
                        <Button
                          size="sm"
                          variant="dangerGhost"
                          title={tr('删除', 'Delete')}
                          aria-label={tr('删除', 'Delete')}
                          onClick={() => handleDelete(route.id)}
                          className="h-7 w-7 p-0"
                        >
                          <Trash2 size={13} />
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

      {/* Create / Edit Modal */}
      <Modal
        open={modalOpen}
        onClose={() => setModalOpen(false)}
        title={editingRoute ? tr('编辑告警路由规则', 'Edit Route Rule') : tr('新建告警路由规则', 'New Route Rule')}
        size="md"
        footer={
          <div className="flex justify-end gap-2 w-full">
            <Button size="sm" variant="ghost" onClick={() => setModalOpen(false)}>
              {tr('取消', 'Cancel')}
            </Button>
            <Button
              size="sm"
              variant="primary"
              onClick={handleSaveRoute}
              disabled={saving || !name.trim()}
            >
              {saving ? tr('保存中...', 'Saving...') : tr('确认保存', 'Save')}
            </Button>
          </div>
        }
      >
        <div className="space-y-4 py-2 text-xs">
          <div>
            <Label className="text-xs">{tr('规则名称', 'Rule Name')} *</Label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. 生产核心支付服务分流规则"
              className="mt-1 text-xs"
            />
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div>
              <Label className="text-xs">{tr('优先级 (数值越小优先级越高)', 'Priority')}</Label>
              <Input
                type="number"
                value={priority}
                onChange={(e) => setPriority(Number(e.target.value))}
                className="mt-1 text-xs"
              />
            </div>
            <div>
              <Label className="text-xs">{tr('目标排班计划', 'Target Schedule')} *</Label>
              <Select
                value={String(targetScheduleId)}
                onValueChange={(val) => setTargetScheduleId(Number(val))}
                options={schedules.map((s) => ({ value: String(s.id), label: s.name }))}
                className="mt-1 w-full"
              />
            </div>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div>
              <Label className="text-xs">{tr('升级策略 (Escalation Policy)', 'Escalation Policy')}</Label>
              <Select
                value={String(escalationPolicyId)}
                onValueChange={(val) => setEscalationPolicyId(Number(val))}
                options={[
                  { value: '0', label: tr('-- 默认直发给在岗人员 --', '-- Direct delivery --') },
                  ...escalationPolicies.map((p) => ({ value: String(p.id), label: p.name })),
                ]}
                className="mt-1 w-full"
              />
            </div>
            <div className="grid grid-cols-2 gap-2">
              <div>
                <Label className="text-xs">{tr('防抖等待 (秒)', 'Wait Sec')}</Label>
                <Input
                  type="number"
                  value={groupWait}
                  onChange={(e) => setGroupWait(Number(e.target.value))}
                  className="mt-1 text-xs"
                />
              </div>
              <div>
                <Label className="text-xs">{tr('聚合周期 (秒)', 'Interval Sec')}</Label>
                <Input
                  type="number"
                  value={groupInterval}
                  onChange={(e) => setGroupInterval(Number(e.target.value))}
                  className="mt-1 text-xs"
                />
              </div>
            </div>
          </div>

          {/* Matchers builder */}
          <div>
            <div className="flex items-center justify-between mb-2">
              <Label className="text-xs">{tr('标签匹配规则 (Label Matchers)', 'Label Matchers')}</Label>
              <Button
                size="sm"
                variant="outline"
                onClick={() =>
                  setMatchers((prev) => [...prev, { name: '', operator: '=', value: '' }])
                }
              >
                <Plus size={12} className="mr-1" />
                {tr('添加条件', 'Add Matcher')}
              </Button>
            </div>

            <div className="space-y-2">
              {matchers.map((m, idx) => (
                <div key={idx} className="flex items-center gap-2">
                  <Input
                    placeholder="tag_name (e.g. service)"
                    value={m.name}
                    onChange={(e) => {
                      const next = [...matchers];
                      next[idx].name = e.target.value;
                      setMatchers(next);
                    }}
                    className="flex-1 text-xs"
                  />
                  <Select
                    value={m.operator}
                    onValueChange={(val) => {
                      const next = [...matchers];
                      next[idx].operator = val as '=' | '!=' | '=~' | '!~';
                      setMatchers(next);
                    }}
                    options={[
                      { value: '=', label: '=' },
                      { value: '!=', label: '!=' },
                      { value: '=~', label: '=~ (Regex)' },
                      { value: '!~', label: '!~ (Not Regex)' },
                    ]}
                    className="w-28 text-xs"
                  />
                  <Input
                    placeholder="value (e.g. payment)"
                    value={m.value}
                    onChange={(e) => {
                      const next = [...matchers];
                      next[idx].value = e.target.value;
                      setMatchers(next);
                    }}
                    className="flex-1 text-xs"
                  />
                  <Button
                    size="icon"
                    variant="ghost"
                    onClick={() => {
                      setMatchers(matchers.filter((_, i) => i !== idx));
                    }}
                    disabled={matchers.length === 1}
                  >
                    <Trash2 size={14} className="text-red-500" />
                  </Button>
                </div>
              ))}
            </div>
          </div>
        </div>
      </Modal>
    </div>
  );
}
