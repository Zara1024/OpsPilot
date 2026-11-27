import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Activity,
  AlertTriangle,
  Coins,
  Loader2,
  Pencil,
  Plus,
  RefreshCw,
  RotateCcw,
  Shield,
  ShieldAlert,
  Trash2,
} from 'lucide-react';
import { ApiError } from '@/api/client';
import { listOrgs, type Org } from '@/api/orgs';
import { listUsers, type User } from '@/api/users';
import {
  createAICostQuota,
  deleteAICostPrice,
  deleteAICostQuota,
  fetchAICostLogs,
  fetchAICostPrices,
  fetchAICostSummary,
  resetAICostCircuit,
  tripAICostCircuit,
  updateAICostQuota,
  upsertAICostPrice,
  type AICostSummary,
  type PriceEntry,
  type QuotaActionOnExceed,
  type QuotaInput,
  type QuotaScopeType,
  type QuotaView,
  type UsageLog,
} from '@/api/aicost';
import { Modal } from '@/components/Modal';
import {
  Button,
  Card,
  Chip,
  EmptyState,
  FilterField,
  Input,
  Label,
  PageHeader,
  PaginationFooter,
  Select,
  Switch,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from '@/components/ui';
import { useDialogs } from '@/components/ui/useDialogs';
import { useI18n } from '@/i18n/locale';
import { usePermissions } from '@/store/me';

type TabKey = 'quotas' | 'usage' | 'prices';

const EMPTY_QUOTA_DRAFT: QuotaInput = {
  name: '',
  scope_type: 'global',
  scope_key: '*',
  enabled: true,
  daily_token_limit: 500000,
  monthly_token_limit: 10000000,
  daily_cost_limit_usd: 10,
  monthly_cost_limit_usd: 200,
  warn_threshold_pct: 80,
  action_on_exceed: 'block',
  description: '',
};

const EMPTY_PRICE_DRAFT = {
  provider: 'openai',
  model_pattern: '',
  input_usd_per_1m: '2.5',
  output_usd_per_1m: '10',
  currency: 'USD',
};

function formatTokens(n: number): string {
  if (!n || n <= 0) return '0';
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}

function formatUSD(v: number): string {
  if (!v || v <= 0) return '$0.00';
  if (v < 0.01) return `$${v.toFixed(4)}`;
  return `$${v.toFixed(2)}`;
}

function sourceLabel(src: string, tr: (zh: string, en: string) => string): string {
  switch (src) {
    case 'chat':
      return tr('智能助手对话', 'Assistant Chat');
    case 'imbridge':
      return tr('IM 机器人联动', 'IM Bridge');
    case 'alert_rca':
      return tr('告警根因调查', 'Alert RCA');
    case 'report':
      return tr('智能运维报告', 'Operational Report');
    case 'flow':
      return tr('工作流编排', 'Workflow');
    case 'query_translate':
      return tr('自然语言转查询', 'NL Query Translate');
    case 'compaction':
      return tr('上下文压缩摘要', 'Context Compaction');
    default:
      return src || tr('其他', 'Other');
  }
}

export default function AdminAICostPage() {
  const { tr } = useI18n();
  const { isAdmin } = usePermissions();
  const { confirmAction, dialog } = useDialogs();

  const [tab, setTab] = useState<TabKey>('quotas');
  const [days, setDays] = useState<number>(7);
  const [summary, setSummary] = useState<AICostSummary | null>(null);
  const [prices, setPrices] = useState<{ custom: PriceEntry[]; builtin: PriceEntry[] }>({ custom: [], builtin: [] });
  const [orgs, setOrgs] = useState<Org[]>([]);
  const [users, setUsers] = useState<User[]>([]);

  const [logs, setLogs] = useState<UsageLog[]>([]);
  const [logsTotal, setLogsTotal] = useState(0);
  const [logsPage, setLogsPage] = useState(0);
  const [logsSourceFilter, setLogsSourceFilter] = useState('all');
  const [logsStatusFilter, setLogsStatusFilter] = useState('all');
  const [logsLoading, setLogsLoading] = useState(false);

  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const [quotaModalOpen, setQuotaModalOpen] = useState(false);
  const [editingQuota, setEditingQuota] = useState<QuotaView | null>(null);
  const [quotaDraft, setQuotaDraft] = useState<QuotaInput>(EMPTY_QUOTA_DRAFT);
  const [quotaSaving, setQuotaSaving] = useState(false);

  const [priceModalOpen, setPriceModalOpen] = useState(false);
  const [priceDraft, setPriceDraft] = useState(EMPTY_PRICE_DRAFT);
  const [priceSaving, setPriceSaving] = useState(false);

  const errMsg = (e: unknown): string => (e instanceof ApiError ? e.message : String(e));

  const orgMap = useMemo(() => new Map(orgs.map((o) => [String(o.id), o.name])), [orgs]);
  const userMap = useMemo(
    () => new Map(users.map((u) => [String(u.id), u.display_name || u.email])),
    [users],
  );

  const loadSummaryAndRefs = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [sumRes, priceRes, orgRes, userRes] = await Promise.all([
        fetchAICostSummary(days),
        fetchAICostPrices(),
        listOrgs().catch(() => ({ items: [] as Org[], total: 0 })),
        listUsers().catch(() => ({ items: [] as User[], total: 0 })),
      ]);
      setSummary(sumRes);
      setPrices(priceRes);
      setOrgs(orgRes.items ?? []);
      setUsers(userRes.items ?? []);
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setLoading(false);
    }
  }, [days]);

  const loadLogs = useCallback(async () => {
    setLogsLoading(true);
    try {
      const res = await fetchAICostLogs({
        days,
        source: logsSourceFilter === 'all' ? undefined : logsSourceFilter,
        status: logsStatusFilter === 'all' ? undefined : logsStatusFilter,
        limit: 20,
        offset: logsPage * 20,
      });
      setLogs(res.items ?? []);
      setLogsTotal(res.total ?? 0);
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setLogsLoading(false);
    }
  }, [days, logsPage, logsSourceFilter, logsStatusFilter]);

  useEffect(() => {
    if (!isAdmin) return;
    void loadSummaryAndRefs();
  }, [isAdmin, loadSummaryAndRefs]);

  useEffect(() => {
    if (!isAdmin || tab !== 'usage') return;
    void loadLogs();
  }, [isAdmin, tab, loadLogs]);

  const openCreateQuota = () => {
    setEditingQuota(null);
    setQuotaDraft(EMPTY_QUOTA_DRAFT);
    setQuotaModalOpen(true);
  };

  const openEditQuota = (q: QuotaView) => {
    setEditingQuota(q);
    setQuotaDraft({
      name: q.name,
      scope_type: q.scope_type,
      scope_key: q.scope_key,
      enabled: q.enabled,
      daily_token_limit: q.daily_token_limit,
      monthly_token_limit: q.monthly_token_limit,
      daily_cost_limit_usd: q.daily_cost_limit_usd,
      monthly_cost_limit_usd: q.monthly_cost_limit_usd,
      warn_threshold_pct: q.warn_threshold_pct,
      action_on_exceed: q.action_on_exceed,
      description: q.description ?? '',
    });
    setQuotaModalOpen(true);
  };

  const handleSaveQuota = async () => {
    setQuotaSaving(true);
    setError(null);
    setNotice(null);
    try {
      const payload: QuotaInput = {
        ...quotaDraft,
        scope_key: quotaDraft.scope_type === 'global' ? '*' : quotaDraft.scope_key.trim(),
      };
      if (editingQuota) {
        await updateAICostQuota(editingQuota.id, payload);
        setNotice(tr(`已更新配额规则「${payload.name}」`, `Updated quota "${payload.name}"`));
      } else {
        await createAICostQuota(payload);
        setNotice(tr(`已创建配额规则「${payload.name}」`, `Created quota "${payload.name}"`));
      }
      setQuotaModalOpen(false);
      await loadSummaryAndRefs();
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setQuotaSaving(false);
    }
  };

  const handleTripQuota = async (q: QuotaView) => {
    const ok = await confirmAction(
      tr(
        `确认手动拉开配额「${q.name}」熔断器？拉开后匹配的 AI 调用将被立即拦截，直到手动恢复。`,
        `Trip circuit breaker for quota "${q.name}"? Matching LLM calls will be blocked until manually reset.`,
      ),
    );
    if (!ok) return;
    setError(null);
    try {
      await tripAICostCircuit(q.id, 'tripped by admin console');
      setNotice(tr(`已手动触发「${q.name}」熔断`, `Tripped circuit for "${q.name}"`));
      await loadSummaryAndRefs();
    } catch (e) {
      setError(errMsg(e));
    }
  };

  const handleResetQuota = async (q: QuotaView) => {
    const ok = await confirmAction(
      tr(`确认复位「${q.name}」熔断器并恢复 AI 调用放行？`, `Reset circuit breaker for "${q.name}"?`),
    );
    if (!ok) return;
    setError(null);
    try {
      await resetAICostCircuit(q.id);
      setNotice(tr(`已复位「${q.name}」熔断器`, `Reset circuit for "${q.name}"`));
      await loadSummaryAndRefs();
    } catch (e) {
      setError(errMsg(e));
    }
  };

  const handleDeleteQuota = async (q: QuotaView) => {
    const ok = await confirmAction(
      tr(`确认删除配额规则「${q.name}」？此操作不可撤销。`, `Delete quota "${q.name}"? This cannot be undone.`),
    );
    if (!ok) return;
    setError(null);
    try {
      await deleteAICostQuota(q.id);
      setNotice(tr(`已删除配额规则「${q.name}」`, `Deleted quota "${q.name}"`));
      await loadSummaryAndRefs();
    } catch (e) {
      setError(errMsg(e));
    }
  };

  const handleSavePrice = async () => {
    setPriceSaving(true);
    setError(null);
    try {
      await upsertAICostPrice({
        provider: priceDraft.provider.trim() || '*',
        model_pattern: priceDraft.model_pattern.trim(),
        input_usd_per_1m: Number(priceDraft.input_usd_per_1m) || 0,
        output_usd_per_1m: Number(priceDraft.output_usd_per_1m) || 0,
        currency: priceDraft.currency.trim() || 'USD',
      });
      setPriceModalOpen(false);
      setNotice(tr('模型单价已保存，后续 AI 调用将立即按新费率核算成本', 'Model pricing saved'));
      await loadSummaryAndRefs();
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setPriceSaving(false);
    }
  };

  const handleDeletePrice = async (p: PriceEntry) => {
    if (!p.id) return;
    const ok = await confirmAction(
      tr(`确认移除自定义单价「${p.provider} / ${p.model_pattern}」并回退内置费率？`, `Delete custom price for ${p.provider} / ${p.model_pattern}?`),
    );
    if (!ok) return;
    setError(null);
    try {
      await deleteAICostPrice(p.id);
      setNotice(tr('已移除自定义模型单价', 'Removed custom model pricing'));
      await loadSummaryAndRefs();
    } catch (e) {
      setError(errMsg(e));
    }
  };

  const scopeBadge = (q: QuotaView): string => {
    switch (q.scope_type) {
      case 'global':
        return tr('全局默认 (*)', 'Global (*)');
      case 'org':
        return `${tr('组织', 'Org')}: ${orgMap.get(q.scope_key) ?? `#${q.scope_key}`}`;
      case 'user':
        return `${tr('用户', 'User')}: ${userMap.get(q.scope_key) ?? `#${q.scope_key}`}`;
      case 'model':
        return `${tr('模型', 'Model')}: ${q.scope_key}`;
      default:
        return `${q.scope_type}:${q.scope_key}`;
    }
  };

  if (!isAdmin) {
    return (
      <div className="p-6">
        <Card className="p-6">
          <EmptyState
            icon={Shield}
            title={tr('需要管理员权限', 'Admin permission required')}
            hint={tr('只有系统管理员才能查看 AI 成本账单、管理 Token 配额或操作熔断器。', 'Only system admins can manage AI cost quotas and circuit breakers.')}
          />
        </Card>
      </div>
    );
  }

  const today = summary?.today;
  const month = summary?.month;
  const quotas = summary?.quotas ?? [];
  const openCount = summary?.open_circuit_count ?? 0;

  return (
    <div className="space-y-5 p-6">
      <PageHeader
        title={tr('AI 成本与熔断', 'AI Cost & Circuit Breakers')}
        subtitle={tr(
          '按组织、用户与模型维度统计 LLM Token 消耗与美元成本，支持自动/手动配额熔断保护',
          'Track token spend across orgs, users, and models with automated and manual budget circuit breakers',
        )}
        className="border-0 px-0 py-0"
        actions={
          <>
            <Select
              aria-label={tr('统计周期', 'Window')}
              value={String(days)}
              onValueChange={(v) => setDays(Number(v))}
              options={[
                { value: '7', label: tr('最近 7 天', 'Last 7 days') },
                { value: '30', label: tr('最近 30 天', 'Last 30 days') },
              ]}
            />
            <Button onClick={() => void loadSummaryAndRefs()} disabled={loading}>
              {loading ? <Loader2 size={12} className="animate-spin" /> : <RefreshCw size={12} />}
              <span>{tr('刷新', 'Refresh')}</span>
            </Button>
            <Button variant="primary" onClick={openCreateQuota}>
              <Plus size={13} />
              <span>{tr('新建配额规则', 'New quota')}</span>
            </Button>
          </>
        }
      />

      {error && (
        <div className="flex items-center justify-between gap-2 rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-2.5 text-xs text-red-600 dark:text-red-300">
          <span>{error}</span>
          <Button size="sm" variant="outline" onClick={() => void loadSummaryAndRefs()}>
            {tr('重试', 'Retry')}
          </Button>
        </div>
      )}
      {notice && (
        <div className="rounded-lg border border-emerald-500/30 bg-emerald-500/10 px-4 py-2.5 text-xs text-emerald-700 dark:text-emerald-300">
          {notice}
        </div>
      )}

      {/* 4 KPI cards */}
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <Card className="p-4">
          <div className="flex items-center justify-between text-xs text-text-muted">
            <span>{tr('今日 Token / 估算成本', 'Today Tokens & Spend')}</span>
            <Coins size={14} className="text-text-faint" />
          </div>
          <div className="mt-2 flex items-baseline gap-2">
            <span className="text-2xl font-semibold text-text">{formatUSD(today?.cost_usd ?? 0)}</span>
            <span className="font-mono text-xs text-text-muted">{formatTokens(today?.total_tokens ?? 0)} tokens</span>
          </div>
          <div className="mt-1 text-[11px] text-text-faint">
            {tr(`共 ${today?.requests ?? 0} 次调用（含失败/拦截）`, `${today?.requests ?? 0} total calls today`)}
          </div>
        </Card>

        <Card className="p-4">
          <div className="flex items-center justify-between text-xs text-text-muted">
            <span>{tr('本月累计 Token / 成本', 'Month-to-Date Spend')}</span>
            <Activity size={14} className="text-text-faint" />
          </div>
          <div className="mt-2 flex items-baseline gap-2">
            <span className="text-2xl font-semibold text-text">{formatUSD(month?.cost_usd ?? 0)}</span>
            <span className="font-mono text-xs text-text-muted">{formatTokens(month?.total_tokens ?? 0)} tokens</span>
          </div>
          <div className="mt-1 text-[11px] text-text-faint">
            {tr(
              `输入 ${formatTokens(month?.prompt_tokens ?? 0)} / 输出 ${formatTokens(month?.completion_tokens ?? 0)}`,
              `In ${formatTokens(month?.prompt_tokens ?? 0)} / Out ${formatTokens(month?.completion_tokens ?? 0)}`,
            )}
          </div>
        </Card>

        <Card className="p-4">
          <div className="flex items-center justify-between text-xs text-text-muted">
            <span>{tr(`近 ${days} 天熔断拦截`, `Blocked Calls (${days}d)`)}</span>
            <ShieldAlert size={14} className="text-text-faint" />
          </div>
          <div className="mt-2 flex items-baseline gap-2">
            <span className="text-2xl font-semibold text-text">{summary?.overview.totals.blocked_count ?? 0}</span>
            <span className="text-xs text-text-muted">
              / {summary?.overview.totals.requests ?? 0} {tr('次请求', 'requests')}
            </span>
          </div>
          <div className="mt-1 text-[11px] text-text-faint">
            {tr('超额触发 block 后将在下个 UTC 周期自动恢复', 'Auto-tripped breakers reset on next UTC cycle')}
          </div>
        </Card>

        <Card className="p-4">
          <div className="flex items-center justify-between text-xs text-text-muted">
            <span>{tr('熔断器全局状态', 'Circuit Breaker Status')}</span>
            <AlertTriangle size={14} className="text-text-faint" />
          </div>
          <div className="mt-2.5 flex items-center gap-2">
            {openCount > 0 ? (
              <Chip tone="danger" className="whitespace-nowrap">
                {tr(`${openCount} 条规则已熔断`, `${openCount} tripped`)}
              </Chip>
            ) : (
              <Chip tone="success" className="whitespace-nowrap">
                {tr('全部放行正常', 'All closed / healthy')}
              </Chip>
            )}
          </div>
          <div className="mt-2 text-[11px] text-text-faint">
            {tr(`已配置 ${quotas.length} 条成本控制策略`, `${quotas.length} active or paused quota rules`)}
          </div>
        </Card>
      </div>

      <Tabs value={tab} onValueChange={(v) => setTab(v as TabKey)}>
        <TabsList>
          <TabsTrigger value="quotas">{tr('配额与熔断规则', 'Quotas & Breakers')}</TabsTrigger>
          <TabsTrigger value="usage">{tr('用量分析与明细', 'Usage & Ledger')}</TabsTrigger>
          <TabsTrigger value="prices">{tr('模型单价配置', 'Model Pricing')}</TabsTrigger>
        </TabsList>

        {/* Tab 1: Quotas */}
        <TabsContent value="quotas" className="mt-4">
          <Card className="p-0">
            {quotas.length === 0 ? (
              <EmptyState
                icon={Coins}
                title={tr('尚未配置 AI 成本配额规则', 'No AI cost quotas configured')}
                hint={tr(
                  '可为全局、特定组织、用户或高价模型（如 claude-opus-*）设置日/月预算上限并启用自动熔断保护。',
                  'Set daily/monthly token or USD budgets by global, org, user, or model wildcard with auto circuit-breaking.',
                )}
                action={
                  <Button variant="primary" onClick={openCreateQuota}>
                    <Plus size={13} />
                    <span>{tr('创建第一条配额规则', 'Create first quota')}</span>
                  </Button>
                }
              />
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-left text-xs">
                  <thead>
                    <tr className="border-b border-border text-[11px] uppercase tracking-wide text-text-muted">
                      <th className="px-4 py-2.5 font-medium">{tr('规则名称', 'Quota')}</th>
                      <th className="px-4 py-2.5 font-medium">{tr('作用域', 'Scope')}</th>
                      <th className="px-4 py-2.5 font-medium">{tr('当日预算消耗', 'Daily Usage')}</th>
                      <th className="px-4 py-2.5 font-medium">{tr('当月预算消耗', 'Monthly Usage')}</th>
                      <th className="whitespace-nowrap px-4 py-2.5 font-medium">{tr('策略 / 状态', 'Policy / State')}</th>
                      <th className="whitespace-nowrap px-4 py-2.5 font-medium">{tr('操作', 'Actions')}</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border">
                    {quotas.map((q) => (
                      <tr key={q.id} className="hover:bg-bg/40">
                        <td className="px-4 py-3 align-top">
                          <div className="font-medium text-text">{q.name}</div>
                          {q.description && <div className="mt-0.5 text-[11px] text-text-muted">{q.description}</div>}
                        </td>
                        <td className="px-4 py-3 align-top">
                          <Chip tone="default" className="whitespace-nowrap font-mono">
                            {scopeBadge(q)}
                          </Chip>
                        </td>
                        <td className="px-4 py-3 align-top font-mono text-[11px] text-text-muted">
                          <div>
                            {formatTokens(q.daily_tokens_used)} / {q.daily_token_limit > 0 ? formatTokens(q.daily_token_limit) : '∞'}
                          </div>
                          <div>
                            {formatUSD(q.daily_cost_used_usd)} / {q.daily_cost_limit_usd > 0 ? formatUSD(q.daily_cost_limit_usd) : '∞'}
                          </div>
                        </td>
                        <td className="px-4 py-3 align-top font-mono text-[11px] text-text-muted">
                          <div>
                            {formatTokens(q.monthly_tokens_used)} / {q.monthly_token_limit > 0 ? formatTokens(q.monthly_token_limit) : '∞'}
                          </div>
                          <div>
                            {formatUSD(q.monthly_cost_used_usd)} / {q.monthly_cost_limit_usd > 0 ? formatUSD(q.monthly_cost_limit_usd) : '∞'}
                          </div>
                        </td>
                        <td className="px-4 py-3 align-top">
                          <div className="flex flex-wrap items-center gap-1.5">
                            {!q.enabled ? (
                              <Chip tone="default" className="whitespace-nowrap">{tr('已停用', 'Disabled')}</Chip>
                            ) : q.circuit_open ? (
                              <Chip tone="danger" className="whitespace-nowrap">{tr('已熔断', 'Circuit Open')}</Chip>
                            ) : q.health_state === 'warn' ? (
                              <Chip tone="warning" className="whitespace-nowrap">{tr('接近上限', 'Warning')}</Chip>
                            ) : (
                              <Chip tone="success" className="whitespace-nowrap">{tr('放行正常', 'Healthy')}</Chip>
                            )}
                            <Chip tone={q.action_on_exceed === 'block' ? 'accent' : 'default'} className="whitespace-nowrap">
                              {q.action_on_exceed === 'block' ? tr('超限阻断', 'Block') : tr('仅告警', 'Warn only')}
                            </Chip>
                          </div>
                          {q.circuit_open && q.circuit_reason && (
                            <div className="mt-1 max-w-xs truncate text-[11px] text-red-500 dark:text-red-400">
                              {q.circuit_reason}
                            </div>
                          )}
                        </td>
                        <td className="whitespace-nowrap px-4 py-3 align-top">
                          <div className="flex items-center gap-1">
                            {q.circuit_open ? (
                              <Button size="sm" variant="outline" onClick={() => void handleResetQuota(q)}>
                                <RotateCcw size={12} />
                                <span>{tr('恢复熔断', 'Reset')}</span>
                              </Button>
                            ) : (
                              <Button size="sm" variant="dangerGhost" onClick={() => void handleTripQuota(q)}>
                                <ShieldAlert size={12} />
                                <span>{tr('手动熔断', 'Trip')}</span>
                              </Button>
                            )}
                            <Button size="sm" onClick={() => openEditQuota(q)}>
                              <Pencil size={12} />
                              <span>{tr('编辑', 'Edit')}</span>
                            </Button>
                            <Button size="sm" variant="dangerGhost" onClick={() => void handleDeleteQuota(q)} aria-label={tr('删除配额', 'Delete quota')}>
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
        </TabsContent>

        {/* Tab 2: Usage Breakdown & Ledger */}
        <TabsContent value="usage" className="mt-4 space-y-4">
          <div className="grid gap-4 lg:grid-cols-2">
            <Card className="p-4">
              <div className="mb-3 text-sm font-medium text-text">{tr('按模型消耗 Top 模型', 'Top Models by Cost')}</div>
              {(summary?.overview.by_model?.length ?? 0) === 0 ? (
                <EmptyState title={tr('暂无模型调用记录', 'No model usage yet')} />
              ) : (
                <div className="space-y-2.5">
                  {summary!.overview.by_model.slice(0, 8).map((row) => {
                    const maxCost = Math.max(...summary!.overview.by_model.map((m) => m.cost_usd), 0.0001);
                    const pct = Math.min(100, Math.round((row.cost_usd / maxCost) * 100));
                    return (
                      <div key={`${row.secondary}-${row.key}`} className="space-y-1 text-xs">
                        <div className="flex items-center justify-between gap-2">
                          <span className="truncate font-mono text-text">
                            {row.secondary ? `${row.secondary} / ` : ''}
                            {row.key}
                          </span>
                          <span className="shrink-0 font-mono text-text-muted">
                            {formatUSD(row.cost_usd)} · {formatTokens(row.total_tokens)} · {row.requests}x
                          </span>
                        </div>
                        <div className="h-1.5 overflow-hidden rounded-full bg-border">
                          <div className="h-full rounded-full bg-indigo-500" style={{ width: `${Math.max(4, pct)}%` }} />
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}
            </Card>

            <Card className="p-4">
              <div className="mb-3 text-sm font-medium text-text">{tr('按业务来源拆分', 'Spend by Business Source')}</div>
              {(summary?.overview.by_source?.length ?? 0) === 0 ? (
                <EmptyState title={tr('暂无业务来源明细', 'No source usage yet')} />
              ) : (
                <div className="space-y-2.5">
                  {summary!.overview.by_source.map((row) => {
                    const maxTok = Math.max(...summary!.overview.by_source.map((s) => s.total_tokens), 1);
                    const pct = Math.min(100, Math.round((row.total_tokens / maxTok) * 100));
                    return (
                      <div key={row.key} className="space-y-1 text-xs">
                        <div className="flex items-center justify-between gap-2">
                          <span className="text-text">{sourceLabel(row.key, tr)}</span>
                          <span className="shrink-0 font-mono text-text-muted">
                            {formatUSD(row.cost_usd)} · {formatTokens(row.total_tokens)} · {row.requests}x
                          </span>
                        </div>
                        <div className="h-1.5 overflow-hidden rounded-full bg-border">
                          <div className="h-full rounded-full bg-emerald-500" style={{ width: `${Math.max(4, pct)}%` }} />
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}
            </Card>
          </div>

          <Card className="p-0">
            <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border px-4 py-3">
              <div className="text-sm font-medium text-text">{tr('LLM 请求成本账本', 'LLM Request Ledger')}</div>
              <div className="flex flex-wrap items-center gap-2">
                <FilterField label={tr('来源', 'Source')}>
                  <Select
                    aria-label={tr('筛选调用来源', 'Filter source')}
                    value={logsSourceFilter}
                    onValueChange={(v) => {
                      setLogsPage(0);
                      setLogsSourceFilter(v);
                    }}
                    options={[
                      { value: 'all', label: tr('全部来源', 'All sources') },
                      { value: 'chat', label: tr('智能助手对话', 'Chat') },
                      { value: 'alert_rca', label: tr('告警根因调查', 'Alert RCA') },
                      { value: 'report', label: tr('运维报告', 'Report') },
                      { value: 'flow', label: tr('工作流', 'Workflow') },
                      { value: 'imbridge', label: tr('IM 机器人', 'IM Bridge') },
                      { value: 'query_translate', label: tr('查询翻译', 'Query Translate') },
                      { value: 'compaction', label: tr('上下文压缩', 'Compaction') },
                    ]}
                  />
                </FilterField>
                <FilterField label={tr('状态', 'Status')}>
                  <Select
                    aria-label={tr('筛选调用状态', 'Filter status')}
                    value={logsStatusFilter}
                    onValueChange={(v) => {
                      setLogsPage(0);
                      setLogsStatusFilter(v);
                    }}
                    options={[
                      { value: 'all', label: tr('全部状态', 'All statuses') },
                      { value: 'ok', label: 'ok' },
                      { value: 'blocked', label: 'blocked' },
                      { value: 'error', label: 'error' },
                    ]}
                  />
                </FilterField>
              </div>
            </div>

            {logsLoading ? (
              <div className="flex items-center justify-center py-12 text-xs text-text-muted">
                <Loader2 size={15} className="mr-2 animate-spin" />
                <span>{tr('正在加载调用流水...', 'Loading ledger...')}</span>
              </div>
            ) : logs.length === 0 ? (
              <EmptyState title={tr('暂无匹配调用记录', 'No matching usage logs')} />
            ) : (
              <>
                <div className="overflow-x-auto">
                  <table className="w-full text-left text-xs">
                    <thead>
                      <tr className="border-b border-border text-[11px] uppercase tracking-wide text-text-muted">
                        <th className="px-4 py-2.5 font-medium">{tr('时间 (UTC)', 'Time')}</th>
                        <th className="px-4 py-2.5 font-medium">{tr('来源', 'Source')}</th>
                        <th className="px-4 py-2.5 font-medium">{tr('提供商 / 模型', 'Provider / Model')}</th>
                        <th className="px-4 py-2.5 font-medium">{tr('归属主体', 'Subject')}</th>
                        <th className="px-4 py-2.5 font-medium">Tokens (In / Out)</th>
                        <th className="px-4 py-2.5 font-medium">{tr('费用 / 耗时', 'Cost / Latency')}</th>
                        <th className="whitespace-nowrap px-4 py-2.5 font-medium">{tr('状态', 'Status')}</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-border">
                      {logs.map((row) => (
                        <tr key={row.id} className="hover:bg-bg/40">
                          <td className="whitespace-nowrap px-4 py-2.5 font-mono text-[11px] text-text-muted">
                            {new Date(row.created_at).toLocaleString()}
                          </td>
                          <td className="whitespace-nowrap px-4 py-2.5">
                            <Chip tone="default" className="whitespace-nowrap">
                              {sourceLabel(row.source, tr)}
                            </Chip>
                          </td>
                          <td className="px-4 py-2.5 font-mono text-[11px] text-text">
                            {row.provider} / {row.model}
                          </td>
                          <td className="px-4 py-2.5 text-[11px] text-text-muted">
                            {row.user_id > 0 ? (userMap.get(String(row.user_id)) ?? `User #${row.user_id}`) : tr('系统后台', 'System')}
                          </td>
                          <td className="whitespace-nowrap px-4 py-2.5 font-mono text-[11px] text-text-muted">
                            {row.prompt_tokens} / {row.completion_tokens}
                          </td>
                          <td className="whitespace-nowrap px-4 py-2.5 font-mono text-[11px] text-text">
                            {formatUSD(row.cost_usd)} · {row.latency_ms}ms
                          </td>
                          <td className="whitespace-nowrap px-4 py-2.5">
                            <Chip
                              tone={row.status === 'ok' ? 'success' : row.status === 'blocked' ? 'warning' : 'danger'}
                              className="whitespace-nowrap"
                            >
                              {row.status}
                            </Chip>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                <PaginationFooter
                  page={logsPage}
                  pageSize={20}
                  shown={logs.length}
                  total={logsTotal}
                  loading={logsLoading}
                  onPageChange={setLogsPage}
                />
              </>
            )}
          </Card>
        </TabsContent>

        {/* Tab 3: Pricing */}
        <TabsContent value="prices" className="mt-4 space-y-4">
          <Card className="p-0">
            <div className="flex items-center justify-between border-b border-border px-4 py-3">
              <div>
                <div className="text-sm font-medium text-text">{tr('自定义模型单价覆盖', 'Custom Model Price Overrides')}</div>
                <div className="text-xs text-text-muted">
                  {tr('私有网关别名或自建开源模型可在此设置每百万 Token 单价（优先级高于内置定价表）', 'Overrides take precedence over built-in pricing')}
                </div>
              </div>
              <Button
                variant="primary"
                onClick={() => {
                  setPriceDraft(EMPTY_PRICE_DRAFT);
                  setPriceModalOpen(true);
                }}
              >
                <Plus size={13} />
                <span>{tr('设置自定义单价', 'Add price override')}</span>
              </Button>
            </div>
            {prices.custom.length === 0 ? (
              <EmptyState
                icon={Coins}
                title={tr('当前未设置自定义单价', 'No custom model prices')}
                hint={tr('点击右上角“设置自定义单价”，或在下方内置参考表点击“覆盖”快速创建。', 'Click above to add a custom rate or override any built-in model below.')}
              />
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-left text-xs">
                  <thead>
                    <tr className="border-b border-border text-[11px] uppercase tracking-wide text-text-muted">
                      <th className="px-4 py-2.5 font-medium">{tr('提供商', 'Provider')}</th>
                      <th className="px-4 py-2.5 font-medium">{tr('模型匹配模式', 'Model Pattern')}</th>
                      <th className="px-4 py-2.5 font-medium">{tr('输入 ($ / 1M)', 'Input ($ / 1M)')}</th>
                      <th className="px-4 py-2.5 font-medium">{tr('输出 ($ / 1M)', 'Output ($ / 1M)')}</th>
                      <th className="whitespace-nowrap px-4 py-2.5 font-medium">{tr('操作', 'Actions')}</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border">
                    {prices.custom.map((p) => (
                      <tr key={`${p.provider}-${p.model_pattern}`} className="hover:bg-bg/40">
                        <td className="px-4 py-2.5 font-mono text-text">{p.provider}</td>
                        <td className="px-4 py-2.5 font-mono text-text">{p.model_pattern}</td>
                        <td className="px-4 py-2.5 font-mono text-text-muted">${p.input_usd_per_1m.toFixed(4)}</td>
                        <td className="px-4 py-2.5 font-mono text-text-muted">${p.output_usd_per_1m.toFixed(4)}</td>
                        <td className="whitespace-nowrap px-4 py-2.5">
                          <div className="flex items-center gap-1">
                            <Button
                              size="sm"
                              onClick={() => {
                                setPriceDraft({
                                  provider: p.provider,
                                  model_pattern: p.model_pattern,
                                  input_usd_per_1m: String(p.input_usd_per_1m),
                                  output_usd_per_1m: String(p.output_usd_per_1m),
                                  currency: p.currency || 'USD',
                                });
                                setPriceModalOpen(true);
                              }}
                            >
                              <Pencil size={12} />
                              <span>{tr('编辑', 'Edit')}</span>
                            </Button>
                            <Button size="sm" variant="dangerGhost" onClick={() => void handleDeletePrice(p)} aria-label={tr('删除单价', 'Delete price')}>
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

          <Card className="p-0">
            <div className="border-b border-border px-4 py-3">
              <div className="text-sm font-medium text-text">{tr('内置参考单价表（USD / 1M Tokens）', 'Built-in Pricing Reference')}</div>
            </div>
            <div className="overflow-x-auto">
              <table className="w-full text-left text-xs">
                <thead>
                  <tr className="border-b border-border text-[11px] uppercase tracking-wide text-text-muted">
                    <th className="px-4 py-2 font-medium">{tr('提供商', 'Provider')}</th>
                    <th className="px-4 py-2 font-medium">{tr('模型匹配模式', 'Model Pattern')}</th>
                    <th className="px-4 py-2 font-medium">{tr('输入 ($ / 1M)', 'Input')}</th>
                    <th className="px-4 py-2 font-medium">{tr('输出 ($ / 1M)', 'Output')}</th>
                    <th className="whitespace-nowrap px-4 py-2 font-medium">{tr('操作', 'Action')}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {prices.builtin.map((p) => (
                    <tr key={`${p.provider}-${p.model_pattern}`} className="hover:bg-bg/40">
                      <td className="px-4 py-2 font-mono text-text-muted">{p.provider}</td>
                      <td className="px-4 py-2 font-mono text-text">{p.model_pattern}</td>
                      <td className="px-4 py-2 font-mono text-text-muted">${p.input_usd_per_1m.toFixed(2)}</td>
                      <td className="px-4 py-2 font-mono text-text-muted">${p.output_usd_per_1m.toFixed(2)}</td>
                      <td className="whitespace-nowrap px-4 py-2">
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => {
                            setPriceDraft({
                              provider: p.provider,
                              model_pattern: p.model_pattern,
                              input_usd_per_1m: String(p.input_usd_per_1m),
                              output_usd_per_1m: String(p.output_usd_per_1m),
                              currency: 'USD',
                            });
                            setPriceModalOpen(true);
                          }}
                        >
                          {tr('覆盖单价', 'Override')}
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Card>
        </TabsContent>
      </Tabs>

      {/* Quota Modal */}
      <Modal
        open={quotaModalOpen}
        onClose={() => setQuotaModalOpen(false)}
        title={editingQuota ? tr(`编辑配额「${editingQuota.name}」`, `Edit Quota "${editingQuota.name}"`) : tr('新建 AI 成本配额与熔断规则', 'New AI Cost Quota')}
        size="lg"
        footer={
          <>
            <Button onClick={() => setQuotaModalOpen(false)}>{tr('取消', 'Cancel')}</Button>
            <Button variant="primary" onClick={() => void handleSaveQuota()} disabled={quotaSaving}>
              {quotaSaving && <Loader2 size={13} className="animate-spin" />}
              <span>{editingQuota ? tr('保存配额', 'Save quota') : tr('创建配额', 'Create quota')}</span>
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <div>
              <Label htmlFor="quota-name" className="mb-1 block text-xs text-text-muted">{tr('规则名称', 'Quota name')}</Label>
              <Input
                id="quota-name"
                value={quotaDraft.name}
                onChange={(e) => setQuotaDraft((d) => ({ ...d, name: e.target.value }))}
                placeholder={tr('例如：全局日消费上限', 'e.g. Global daily cap')}
                className="w-full"
              />
            </div>
            <div>
              <Label htmlFor="quota-action" className="mb-1 block text-xs text-text-muted">{tr('超限处理策略', 'Action on exceed')}</Label>
              <Select
                id="quota-action"
                value={quotaDraft.action_on_exceed}
                onValueChange={(v) => setQuotaDraft((d) => ({ ...d, action_on_exceed: v as QuotaActionOnExceed }))}
                options={[
                  { value: 'block', label: tr('阻断并触发熔断 (block)', 'Block & trip circuit') },
                  { value: 'warn', label: tr('仅告警放行 (warn)', 'Warn only') },
                ]}
                className="w-full"
              />
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <div>
              <Label htmlFor="quota-scope-type" className="mb-1 block text-xs text-text-muted">{tr('作用域类型', 'Scope type')}</Label>
              <Select
                id="quota-scope-type"
                value={quotaDraft.scope_type}
                onValueChange={(v) => {
                  const nextType = v as QuotaScopeType;
                  let nextKey = '*';
                  if (nextType === 'org') nextKey = String(orgs[0]?.id ?? '');
                  if (nextType === 'user') nextKey = String(users[0]?.id ?? '');
                  if (nextType === 'model') nextKey = 'gpt-4o*';
                  setQuotaDraft((d) => ({ ...d, scope_type: nextType, scope_key: nextKey }));
                }}
                options={[
                  { value: 'global', label: tr('全局范围 (global)', 'Global') },
                  { value: 'org', label: tr('指定组织 (org)', 'Organization') },
                  { value: 'user', label: tr('指定用户 (user)', 'User') },
                  { value: 'model', label: tr('模型通配符 (model)', 'Model pattern') },
                ]}
                className="w-full"
              />
            </div>
            <div>
              <Label htmlFor="quota-scope-key" className="mb-1 block text-xs text-text-muted">{tr('作用域目标', 'Target key')}</Label>
              {quotaDraft.scope_type === 'org' ? (
                <Select
                  id="quota-scope-key"
                  value={quotaDraft.scope_key}
                  onValueChange={(v) => setQuotaDraft((d) => ({ ...d, scope_key: v }))}
                  options={orgs.map((o) => ({ value: String(o.id), label: o.name }))}
                  className="w-full"
                />
              ) : quotaDraft.scope_type === 'user' ? (
                <Select
                  id="quota-scope-key"
                  value={quotaDraft.scope_key}
                  onValueChange={(v) => setQuotaDraft((d) => ({ ...d, scope_key: v }))}
                  options={users.map((u) => ({ value: String(u.id), label: `${u.display_name || u.email} (${u.email})` }))}
                  className="w-full"
                />
              ) : (
                <Input
                  id="quota-scope-key"
                  value={quotaDraft.scope_type === 'global' ? '*' : quotaDraft.scope_key}
                  disabled={quotaDraft.scope_type === 'global'}
                  onChange={(e) => setQuotaDraft((d) => ({ ...d, scope_key: e.target.value }))}
                  placeholder="gpt-4o*"
                  className="w-full font-mono"
                />
              )}
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <div>
              <Label htmlFor="quota-daily-tokens" className="mb-1 block text-xs text-text-muted">{tr('每日 Token 上限 (0=不限)', 'Daily token limit (0=∞)')}</Label>
              <Input
                id="quota-daily-tokens"
                type="number"
                value={String(quotaDraft.daily_token_limit)}
                onChange={(e) => setQuotaDraft((d) => ({ ...d, daily_token_limit: Number(e.target.value) || 0 }))}
                className="w-full font-mono"
              />
            </div>
            <div>
              <Label htmlFor="quota-monthly-tokens" className="mb-1 block text-xs text-text-muted">{tr('每月 Token 上限 (0=不限)', 'Monthly token limit (0=∞)')}</Label>
              <Input
                id="quota-monthly-tokens"
                type="number"
                value={String(quotaDraft.monthly_token_limit)}
                onChange={(e) => setQuotaDraft((d) => ({ ...d, monthly_token_limit: Number(e.target.value) || 0 }))}
                className="w-full font-mono"
              />
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-3">
            <div>
              <Label htmlFor="quota-daily-usd" className="mb-1 block text-xs text-text-muted">{tr('每日美元上限 ($)', 'Daily cost USD (0=∞)')}</Label>
              <Input
                id="quota-daily-usd"
                type="number"
                step="0.01"
                value={String(quotaDraft.daily_cost_limit_usd)}
                onChange={(e) => setQuotaDraft((d) => ({ ...d, daily_cost_limit_usd: Number(e.target.value) || 0 }))}
                className="w-full font-mono"
              />
            </div>
            <div>
              <Label htmlFor="quota-monthly-usd" className="mb-1 block text-xs text-text-muted">{tr('每月美元上限 ($)', 'Monthly cost USD (0=∞)')}</Label>
              <Input
                id="quota-monthly-usd"
                type="number"
                step="0.01"
                value={String(quotaDraft.monthly_cost_limit_usd)}
                onChange={(e) => setQuotaDraft((d) => ({ ...d, monthly_cost_limit_usd: Number(e.target.value) || 0 }))}
                className="w-full font-mono"
              />
            </div>
            <div>
              <Label htmlFor="quota-warn-pct" className="mb-1 block text-xs text-text-muted">{tr('预警水位 (%)', 'Warn threshold (%)')}</Label>
              <Input
                id="quota-warn-pct"
                type="number"
                value={String(quotaDraft.warn_threshold_pct)}
                onChange={(e) => setQuotaDraft((d) => ({ ...d, warn_threshold_pct: Number(e.target.value) || 80 }))}
                className="w-full font-mono"
              />
            </div>
          </div>

          <div className="flex items-center justify-between rounded-lg border border-border px-3 py-2.5">
            <div>
              <div className="text-xs font-medium text-text">{tr('启用该配额策略', 'Enable quota enforcement')}</div>
              <div className="text-[11px] text-text-muted">{tr('停用后将保留历史配置但暂不参与请求校验', 'Paused quotas are preserved without blocking calls')}</div>
            </div>
            <Switch
              checked={quotaDraft.enabled}
              onCheckedChange={(checked) => setQuotaDraft((d) => ({ ...d, enabled: checked }))}
              aria-label={tr('启用该配额策略', 'Enable quota')}
            />
          </div>
        </div>
      </Modal>

      {/* Price Modal */}
      <Modal
        open={priceModalOpen}
        onClose={() => setPriceModalOpen(false)}
        title={tr('设置自定义模型单价', 'Upsert Model Price')}
        size="md"
        footer={
          <>
            <Button onClick={() => setPriceModalOpen(false)}>{tr('取消', 'Cancel')}</Button>
            <Button variant="primary" onClick={() => void handleSavePrice()} disabled={priceSaving}>
              {priceSaving && <Loader2 size={13} className="animate-spin" />}
              <span>{tr('保存单价', 'Save price')}</span>
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <div>
              <Label htmlFor="price-provider" className="mb-1 block text-xs text-text-muted">{tr('提供商 ID', 'Provider')}</Label>
              <Select
                id="price-provider"
                value={priceDraft.provider}
                onValueChange={(v) => setPriceDraft((d) => ({ ...d, provider: v }))}
                options={[
                  { value: '*', label: '* (All)' },
                  { value: 'openai', label: 'openai' },
                  { value: 'anthropic', label: 'anthropic' },
                  { value: 'gemini', label: 'gemini' },
                  { value: 'deepseek', label: 'deepseek' },
                  { value: 'zhipu', label: 'zhipu' },
                  { value: 'kimi', label: 'kimi' },
                  { value: 'minimax', label: 'minimax' },
                  { value: 'xiaomi', label: 'xiaomi' },
                  { value: 'custom', label: 'custom' },
                ]}
                className="w-full"
              />
            </div>
            <div>
              <Label htmlFor="price-pattern" className="mb-1 block text-xs text-text-muted">{tr('模型匹配模式', 'Model pattern')}</Label>
              <Input
                id="price-pattern"
                value={priceDraft.model_pattern}
                onChange={(e) => setPriceDraft((d) => ({ ...d, model_pattern: e.target.value }))}
                placeholder="gpt-4o* / qwen-72b*"
                className="w-full font-mono"
              />
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <div>
              <Label htmlFor="price-in" className="mb-1 block text-xs text-text-muted">{tr('输入单价 ($ / 1M Tokens)', 'Input ($ / 1M)')}</Label>
              <Input
                id="price-in"
                type="number"
                step="0.0001"
                value={priceDraft.input_usd_per_1m}
                onChange={(e) => setPriceDraft((d) => ({ ...d, input_usd_per_1m: e.target.value }))}
                className="w-full font-mono"
              />
            </div>
            <div>
              <Label htmlFor="price-out" className="mb-1 block text-xs text-text-muted">{tr('输出单价 ($ / 1M Tokens)', 'Output ($ / 1M)')}</Label>
              <Input
                id="price-out"
                type="number"
                step="0.0001"
                value={priceDraft.output_usd_per_1m}
                onChange={(e) => setPriceDraft((d) => ({ ...d, output_usd_per_1m: e.target.value }))}
                className="w-full font-mono"
              />
            </div>
          </div>
        </div>
      </Modal>

      {dialog}
    </div>
  );
}
