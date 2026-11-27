import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  CheckCircle2,
  FlaskConical,
  Loader2,
  Pencil,
  Plus,
  RefreshCw,
  Shield,
  ShieldAlert,
  Trash2,
  XCircle,
} from 'lucide-react';
import { ApiError } from '@/api/client';
import { listOrgs, type Org } from '@/api/orgs';
import {
  checkResourceAccess,
  createResourceGrant,
  deleteResourceGrant,
  listResourceGrants,
  updateResourceGrant,
  type RbacDecision,
  type RbacEffect,
  type RbacResourceType,
  type RbacSubjectType,
  type ResourceGrant,
  type ResourceGrantInput,
} from '@/api/rbac';
import { listUsers, type User } from '@/api/users';
import { Modal } from '@/components/Modal';
import {
  Button,
  Card,
  Checkbox,
  Chip,
  EmptyState,
  FilterField,
  Input,
  Label,
  PageHeader,
  Select,
} from '@/components/ui';
import { useDialogs } from '@/components/ui/useDialogs';
import { useI18n } from '@/i18n/locale';
import { useAssistantPage } from '@/lib/assistantPage';
import { usePermissions } from '@/store/me';

const ALL_ACTIONS = ['read', 'write', 'exec', 'manage', '*'] as const;

type GrantDraft = {
  subject_type: RbacSubjectType;
  subject_id: number;
  resource_type: RbacResourceType;
  resource_pattern: string;
  selectedActions: string[];
  effect: RbacEffect;
  description: string;
};

function defaultGrantDraft(users: User[], orgs: Org[]): GrantDraft {
  return {
    subject_type: 'user',
    subject_id: users[0]?.id ?? orgs[0]?.id ?? 0,
    resource_type: 'device_env',
    resource_pattern: 'prod*',
    selectedActions: ['exec'],
    effect: 'deny',
    description: '',
  };
}

export default function AdminRolesPage() {
  const { tr } = useI18n();
  const { isAdmin } = usePermissions();
  const { confirmAction, dialog } = useDialogs();

  const [grants, setGrants] = useState<ResourceGrant[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [orgs, setOrgs] = useState<Org[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  // Filters
  const [resourceFilter, setResourceFilter] = useState<string>('all');
  const [effectFilter, setEffectFilter] = useState<string>('all');
  const [search, setSearch] = useState('');

  // Create / edit modal
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<ResourceGrant | null>(null);
  const [draft, setDraft] = useState<GrantDraft>(() => defaultGrantDraft([], []));
  const [modalSaving, setModalSaving] = useState(false);
  const [modalError, setModalError] = useState<string | null>(null);

  // Simulator state
  const [simUserId, setSimUserId] = useState<number>(0);
  const [simResourceType, setSimResourceType] = useState<RbacResourceType>('device_env');
  const [simKeysRaw, setSimKeysRaw] = useState<string>('production');
  const [simAction, setSimAction] = useState<string>('exec');
  const [simRunning, setSimRunning] = useState(false);
  const [simResult, setSimResult] = useState<RbacDecision | null>(null);

  const loadAll = useCallback(async () => {
    if (!isAdmin) return;
    setLoading(true);
    setError(null);
    try {
      const [gResp, uResp, oResp] = await Promise.all([
        listResourceGrants(),
        listUsers(),
        listOrgs(),
      ]);
      const loadedUsers = uResp.items ?? [];
      setGrants(gResp.items ?? []);
      setUsers(loadedUsers);
      setOrgs(oResp.items ?? []);
      setSimUserId((prev) => prev || loadedUsers.find((u) => u.role !== 'admin')?.id || loadedUsers[0]?.id || 0);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : (e as Error).message);
    } finally {
      setLoading(false);
    }
  }, [isAdmin]);

  useEffect(() => {
    void loadAll();
  }, [loadAll]);

  useAssistantPage('资源权限', 'Resource RBAC', {
    grant_count: grants.length,
  });

  const userById = useMemo(() => new Map(users.map((u) => [u.id, u])), [users]);
  const orgById = useMemo(() => new Map(orgs.map((o) => [o.id, o])), [orgs]);

  const subjectLabel = useCallback(
    (st: RbacSubjectType, id: number) => {
      if (st === 'user') {
        const u = userById.get(id);
        return u ? `${u.display_name || u.email} (${u.email})` : `User #${id}`;
      }
      const o = orgById.get(id);
      return o ? o.name : `Org #${id}`;
    },
    [orgById, userById],
  );

  const resourceTypeLabel = (rt: RbacResourceType) => {
    switch (rt) {
      case 'device':
        return tr('主机 / 设备', 'Device / Host');
      case 'device_env':
        return tr('设备环境标签', 'Device environment');
      case 'k8s_cluster':
        return tr('K8s 集群', 'K8s cluster');
      case 'service':
        return tr('APM 服务', 'APM service');
    }
  };

  const filteredGrants = useMemo(() => {
    const q = search.trim().toLowerCase();
    return grants.filter((g) => {
      if (resourceFilter !== 'all' && g.resource_type !== resourceFilter) return false;
      if (effectFilter !== 'all' && g.effect !== effectFilter) return false;
      if (!q) return true;
      const subj = subjectLabel(g.subject_type, g.subject_id).toLowerCase();
      return (
        subj.includes(q) ||
        g.resource_pattern.toLowerCase().includes(q) ||
        (g.description ?? '').toLowerCase().includes(q)
      );
    });
  }, [effectFilter, grants, resourceFilter, search, subjectLabel]);

  const hasActiveFilter = resourceFilter !== 'all' || effectFilter !== 'all' || search.trim() !== '';

  function openCreate() {
    setEditing(null);
    setDraft(defaultGrantDraft(users, orgs));
    setModalError(null);
    setModalOpen(true);
  }

  function openEdit(g: ResourceGrant) {
    setEditing(g);
    setDraft({
      subject_type: g.subject_type,
      subject_id: g.subject_id,
      resource_type: g.resource_type,
      resource_pattern: g.resource_pattern,
      selectedActions: g.actions.split(',').map((a) => a.trim()).filter(Boolean),
      effect: g.effect,
      description: g.description ?? '',
    });
    setModalError(null);
    setModalOpen(true);
  }

  function toggleAction(act: string, checked: boolean) {
    setDraft((prev) => {
      if (act === '*') {
        return { ...prev, selectedActions: checked ? ['*'] : ['read'] };
      }
      const next = prev.selectedActions.filter((x) => x !== '*' && x !== act);
      if (checked) next.push(act);
      return { ...prev, selectedActions: next };
    });
  }

  async function submitGrant() {
    if (!draft.subject_id) {
      setModalError(tr('请选择授权主体', 'Select a user or org'));
      return;
    }
    if (draft.selectedActions.length === 0) {
      setModalError(tr('至少选择一个动作', 'Select at least one action'));
      return;
    }
    const payload: ResourceGrantInput = {
      subject_type: draft.subject_type,
      subject_id: draft.subject_id,
      resource_type: draft.resource_type,
      resource_pattern: draft.resource_pattern.trim(),
      actions: draft.selectedActions.join(','),
      effect: draft.effect,
      description: draft.description.trim(),
    };
    setModalSaving(true);
    setModalError(null);
    try {
      if (editing) {
        const updated = await updateResourceGrant(editing.id, payload);
        setGrants((cur) => cur.map((x) => (x.id === updated.id ? updated : x)));
        setNotice(tr(`已更新规则 #${updated.id}`, `Updated rule #${updated.id}`));
      } else {
        const created = await createResourceGrant(payload);
        setGrants((cur) => [created, ...cur]);
        setNotice(tr(`已创建规则 #${created.id}`, `Created rule #${created.id}`));
      }
      setModalOpen(false);
    } catch (e) {
      setModalError(e instanceof ApiError ? e.message : (e as Error).message);
    } finally {
      setModalSaving(false);
    }
  }

  async function removeGrant(g: ResourceGrant) {
    const ok = await confirmAction(
      tr(
        `确认删除规则 #${g.id}（${g.effect.toUpperCase()} ${g.resource_type}:${g.resource_pattern} [${g.actions}]）？`,
        `Delete rule #${g.id} (${g.effect.toUpperCase()} ${g.resource_type}:${g.resource_pattern} [${g.actions}])?`,
      ),
    );
    if (!ok) return;
    try {
      await deleteResourceGrant(g.id);
      setGrants((cur) => cur.filter((x) => x.id !== g.id));
      setNotice(tr(`已删除规则 #${g.id}`, `Deleted rule #${g.id}`));
    } catch (e) {
      setError(e instanceof ApiError ? e.message : (e as Error).message);
    }
  }

  async function runSimulator() {
    if (!simUserId) return;
    setSimRunning(true);
    setError(null);
    try {
      const keys = simKeysRaw
        .split(',')
        .map((k) => k.trim())
        .filter(Boolean);
      const dec = await checkResourceAccess({
        user_id: simUserId,
        resource_type: simResourceType,
        resource_keys: keys,
        action: simAction,
      });
      setSimResult(dec);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : (e as Error).message);
    } finally {
      setSimRunning(false);
    }
  }

  if (!isAdmin) {
    return (
      <div className="p-6">
        <Card className="p-6">
          <EmptyState
            icon={Shield}
            title={tr('需要管理员权限', 'Admin permission required')}
            hint={tr('只有系统管理员才能配置资源级访问控制策略。', 'Only system admins can configure resource-level RBAC rules.')}
          />
        </Card>
      </div>
    );
  }

  return (
    <div className="space-y-5 p-6">
      <PageHeader
        title={tr('资源权限 (RBAC)', 'Resource Access (RBAC)')}
        subtitle={tr(
          '按主机、环境标签、Kubernetes 集群或 APM 服务粒度授予或显式禁止访问权限；显式 Deny 优先级高于 Allow',
          'Scope access by device, environment, K8s cluster, or APM service; explicit Deny always overrides Allow',
        )}
        className="border-0 px-0 py-0"
        actions={
          <>
            <Button onClick={() => void loadAll()} disabled={loading}>
              {loading ? <Loader2 size={12} className="animate-spin" /> : <RefreshCw size={12} />}
              <span>{tr('刷新', 'Refresh')}</span>
            </Button>
            <Button variant="primary" onClick={openCreate}>
              <Plus size={13} />
              <span>{tr('新建授权规则', 'New grant')}</span>
            </Button>
          </>
        }
      />

      {error && (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-2.5 text-xs text-red-600 dark:text-red-300">
          {error}
        </div>
      )}
      {notice && (
        <div className="rounded-lg border border-emerald-500/30 bg-emerald-500/10 px-4 py-2.5 text-xs text-emerald-700 dark:text-emerald-300">
          {notice}
        </div>
      )}

      {/* Rules list card */}
      <Card className="p-0">
        <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
          <FilterField label={tr('资源类型', 'Resource')}>
            <Select
              aria-label={tr('筛选资源类型', 'Filter by resource type')}
              value={resourceFilter}
              onValueChange={setResourceFilter}
              options={[
                { value: 'all', label: tr('全部类型', 'All resources') },
                { value: 'device', label: tr('主机 / 设备', 'Device') },
                { value: 'device_env', label: tr('设备环境', 'Device env') },
                { value: 'k8s_cluster', label: tr('K8s 集群', 'K8s cluster') },
                { value: 'service', label: tr('APM 服务', 'APM service') },
              ]}
            />
          </FilterField>

          <FilterField label={tr('效力', 'Effect')}>
            <Select
              aria-label={tr('筛选规则效力', 'Filter by effect')}
              value={effectFilter}
              onValueChange={setEffectFilter}
              options={[
                { value: 'all', label: tr('全部', 'All') },
                { value: 'allow', label: 'allow' },
                { value: 'deny', label: 'deny' },
              ]}
            />
          </FilterField>

          <Input
            type="search"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={tr('搜索主体、模式或备注…', 'Search subject, pattern, description…')}
            className="min-w-[220px] flex-1"
          />
        </div>

        {grants.length === 0 ? (
          <EmptyState
            icon={ShieldAlert}
            title={tr('尚未配置资源级授权规则', 'No resource-level RBAC rules')}
            hint={tr('未配置规则时系统沿用组织角色默认权限；添加 Allow 白名单即可实现资源隔离，或添加 Deny 禁止高危操作（如禁止生产 WebSSH）。', 'Without rules, org roles govern access. Add Allow whitelists to isolate teams or Deny rules to block sensitive targets.')}
            action={
              <Button variant="primary" onClick={openCreate}>
                <Plus size={13} />
                <span>{tr('创建第一条规则', 'Create rule')}</span>
              </Button>
            }
          />
        ) : filteredGrants.length === 0 ? (
          <EmptyState
            icon={Shield}
            title={tr('没有匹配的规则', 'No matching rules')}
            hint={hasActiveFilter ? tr('尝试调整上方筛选条件。', 'Try clearing or loosening filters above.') : undefined}
            action={
              hasActiveFilter ? (
                <Button
                  variant="outline"
                  onClick={() => {
                    setResourceFilter('all');
                    setEffectFilter('all');
                    setSearch('');
                  }}
                >
                  {tr('清除筛选', 'Clear filters')}
                </Button>
              ) : undefined
            }
          />
        ) : (
          <div className="overflow-x-auto">
            <table className="og-resource-table">
              <thead>
                <tr>
                  <th>{tr('授权主体', 'Subject')}</th>
                  <th>{tr('资源类型', 'Resource type')}</th>
                  <th>{tr('匹配模式', 'Pattern')}</th>
                  <th>{tr('动作', 'Actions')}</th>
                  <th>{tr('效力', 'Effect')}</th>
                  <th>{tr('备注', 'Description')}</th>
                  <th>{tr('操作', 'Actions')}</th>
                </tr>
              </thead>
              <tbody>
                {filteredGrants.map((g) => (
                  <tr key={g.id}>
                    <td>
                      <div className="flex items-center gap-1.5">
                        <Chip dense tone={g.subject_type === 'org' ? 'info' : 'default'}>
                          {g.subject_type === 'org' ? tr('组织', 'Org') : tr('用户', 'User')}
                        </Chip>
                        <span className="truncate text-xs font-medium text-text">
                          {subjectLabel(g.subject_type, g.subject_id)}
                        </span>
                      </div>
                    </td>
                    <td className="text-xs text-text-muted">{resourceTypeLabel(g.resource_type)}</td>
                    <td className="font-mono text-xs text-text">{g.resource_pattern}</td>
                    <td>
                      <div className="flex flex-wrap gap-1">
                        {g.actions.split(',').map((a) => (
                          <Chip key={a} dense tone="default">{a.trim()}</Chip>
                        ))}
                      </div>
                    </td>
                    <td>
                      <Chip tone={g.effect === 'deny' ? 'danger' : 'success'}>
                        {g.effect.toUpperCase()}
                      </Chip>
                    </td>
                    <td className="max-w-[200px] truncate text-xs text-text-muted">
                      {g.description || '—'}
                    </td>
                    <td>
                      <div className="flex items-center gap-1">
                        <Button size="sm" variant="outline" onClick={() => openEdit(g)}>
                          <Pencil size={12} />
                          <span>{tr('编辑', 'Edit')}</span>
                        </Button>
                        <Button size="sm" variant="dangerGhost" onClick={() => void removeGrant(g)}>
                          <Trash2 size={12} />
                          <span>{tr('删除', 'Delete')}</span>
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

      {/* Permission Simulator */}
      <Card className="space-y-4 p-5">
        <div className="flex items-center gap-2 border-b border-border pb-3">
          <FlaskConical size={15} className="text-indigo-500" />
          <div>
            <h2 className="text-sm font-semibold text-text">{tr('权限决策模拟器', 'Access Policy Simulator')}</h2>
            <p className="text-xs text-text-muted">
              {tr('实时演练任意用户对指定资源目标的最终判定结果（包含其所属组织继承规则）', 'Simulate effective access decisions including org-inherited grants')}
            </p>
          </div>
        </div>

        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-12">
          <div className="lg:col-span-3">
            <Label htmlFor="sim-user" className="mb-1 block text-xs text-text-muted">{tr('测试用户', 'User')}</Label>
            <Select
              id="sim-user"
              value={simUserId ? String(simUserId) : ''}
              onValueChange={(v) => setSimUserId(Number(v))}
              options={users.map((u) => ({
                value: String(u.id),
                label: `${u.display_name || u.email} (${u.role})`,
              }))}
              className="w-full"
            />
          </div>
          <div className="lg:col-span-3">
            <Label htmlFor="sim-rtype" className="mb-1 block text-xs text-text-muted">{tr('资源类型', 'Resource type')}</Label>
            <Select
              id="sim-rtype"
              value={simResourceType}
              onValueChange={(v) => setSimResourceType(v as RbacResourceType)}
              options={[
                { value: 'device_env', label: tr('设备环境 (device_env)', 'device_env') },
                { value: 'device', label: tr('主机设备 (device)', 'device') },
                { value: 'k8s_cluster', label: tr('K8s 集群 (k8s_cluster)', 'k8s_cluster') },
                { value: 'service', label: tr('APM 服务 (service)', 'service') },
              ]}
              className="w-full"
            />
          </div>
          <div className="lg:col-span-2">
            <Label htmlFor="sim-keys" className="mb-1 block text-xs text-text-muted">{tr('目标标识（逗号分隔）', 'Target keys')}</Label>
            <Input
              id="sim-keys"
              value={simKeysRaw}
              onChange={(e) => setSimKeysRaw(e.target.value)}
              placeholder="production, payment-api"
              className="w-full font-mono"
            />
          </div>
          <div className="lg:col-span-2">
            <Label htmlFor="sim-action" className="mb-1 block text-xs text-text-muted">{tr('操作动作', 'Action')}</Label>
            <Select
              id="sim-action"
              value={simAction}
              onValueChange={setSimAction}
              options={[
                { value: 'read', label: 'read' },
                { value: 'write', label: 'write' },
                { value: 'exec', label: 'exec (WebSSH)' },
                { value: 'manage', label: 'manage' },
              ]}
              className="w-full"
            />
          </div>
          <div className="flex items-end lg:col-span-2">
            <Button variant="primary" onClick={() => void runSimulator()} disabled={simRunning || !simUserId} className="w-full justify-center">
              {simRunning ? <Loader2 size={13} className="animate-spin" /> : <FlaskConical size={13} />}
              <span>{tr('测试权限', 'Check access')}</span>
            </Button>
          </div>
        </div>

        {simResult && (
          <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border bg-bg/60 px-4 py-3 text-xs">
            <div className="flex items-center gap-2">
              {simResult.allowed ? (
                <CheckCircle2 size={16} className="text-emerald-500" />
              ) : (
                <XCircle size={16} className="text-red-500" />
              )}
              <Chip tone={simResult.allowed ? 'success' : 'danger'}>
                {simResult.allowed ? tr('允许访问 (ALLOW)', 'ALLOW') : tr('拒绝访问 (DENY)', 'DENY')}
              </Chip>
              <span className="font-mono text-text-muted">{simResult.reason}</span>
            </div>
            {simResult.matched_rule && (
              <div className="font-mono text-[11px] text-text-muted">
                {tr('命中规则：', 'Matched: ')}
                #{simResult.matched_grant_id} ({simResult.matched_rule})
              </div>
            )}
          </div>
        )}
      </Card>

      {/* Grant Modal */}
      <Modal
        open={modalOpen}
        onClose={() => setModalOpen(false)}
        title={editing ? tr(`编辑资源授权 #${editing.id}`, `Edit RBAC Grant #${editing.id}`) : tr('新建资源授权规则', 'New Resource RBAC Grant')}
        size="lg"
        footer={
          <>
            <Button onClick={() => setModalOpen(false)}>{tr('取消', 'Cancel')}</Button>
            <Button variant="primary" onClick={() => void submitGrant()} disabled={modalSaving}>
              {modalSaving && <Loader2 size={13} className="animate-spin" />}
              <span>{editing ? tr('保存规则', 'Save grant') : tr('创建规则', 'Create grant')}</span>
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <div>
              <Label htmlFor="grant-subj-type" className="mb-1 block text-xs text-text-muted">{tr('主体类型', 'Subject type')}</Label>
              <Select
                id="grant-subj-type"
                value={draft.subject_type}
                onValueChange={(v) => {
                  const st = v as RbacSubjectType;
                  const nextId = st === 'user' ? (users[0]?.id ?? 0) : (orgs[0]?.id ?? 0);
                  setDraft((d) => ({ ...d, subject_type: st, subject_id: nextId }));
                }}
                options={[
                  { value: 'user', label: tr('指定用户 (User)', 'User') },
                  { value: 'org', label: tr('整个组织 (Org)', 'Organization') },
                ]}
                className="w-full"
              />
            </div>

            <div>
              <Label htmlFor="grant-subj-id" className="mb-1 block text-xs text-text-muted">{tr('目标用户 / 组织', 'Target subject')}</Label>
              <Select
                id="grant-subj-id"
                value={draft.subject_id ? String(draft.subject_id) : ''}
                onValueChange={(v) => setDraft((d) => ({ ...d, subject_id: Number(v) }))}
                options={
                  draft.subject_type === 'user'
                    ? users.map((u) => ({ value: String(u.id), label: `${u.display_name || u.email} (${u.email})` }))
                    : orgs.map((o) => ({ value: String(o.id), label: o.name }))
                }
                className="w-full"
              />
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <div>
              <Label htmlFor="grant-rtype" className="mb-1 block text-xs text-text-muted">{tr('资源类别', 'Resource type')}</Label>
              <Select
                id="grant-rtype"
                value={draft.resource_type}
                onValueChange={(v) => setDraft((d) => ({ ...d, resource_type: v as RbacResourceType }))}
                options={[
                  { value: 'device_env', label: tr('设备环境 (device_env)', 'device_env') },
                  { value: 'device', label: tr('主机设备 (device)', 'device') },
                  { value: 'k8s_cluster', label: tr('K8s 集群 (k8s_cluster)', 'k8s_cluster') },
                  { value: 'service', label: tr('APM 服务 (service)', 'service') },
                ]}
                className="w-full"
              />
            </div>

            <div>
              <Label htmlFor="grant-effect" className="mb-1 block text-xs text-text-muted">{tr('策略效力', 'Effect')}</Label>
              <Select
                id="grant-effect"
                value={draft.effect}
                onValueChange={(v) => setDraft((d) => ({ ...d, effect: v as RbacEffect }))}
                options={[
                  { value: 'allow', label: tr('允许 (ALLOW 白名单)', 'ALLOW (whitelist)') },
                  { value: 'deny', label: tr('拒绝 (DENY 强制拦截)', 'DENY (explicit block)') },
                ]}
                className="w-full"
              />
            </div>
          </div>

          <div>
            <Label htmlFor="grant-pattern" className="mb-1 block text-xs text-text-muted">
              {tr('资源匹配模式（支持通配符 * 与 ?）', 'Resource pattern (supports * and ? globs)')}
            </Label>
            <Input
              id="grant-pattern"
              value={draft.resource_pattern}
              onChange={(e) => setDraft((d) => ({ ...d, resource_pattern: e.target.value }))}
              placeholder={tr('例如：prod*、staging、payment-*、*', 'e.g. prod*, staging, payment-*, *')}
              className="w-full font-mono"
            />
          </div>

          <div>
            <Label className="mb-1.5 block text-xs text-text-muted">{tr('包含动作', 'Actions')}</Label>
            <div className="flex flex-wrap gap-3 rounded-lg border border-border bg-bg/50 px-3 py-2.5">
              {ALL_ACTIONS.map((act) => (
                <label key={act} className="flex cursor-pointer items-center gap-1.5 text-xs text-text">
                  <Checkbox
                    checked={draft.selectedActions.includes(act)}
                    onCheckedChange={(checked) => toggleAction(act, Boolean(checked))}
                    aria-label={act}
                  />
                  <span className="font-mono">{act === '*' ? tr('* (全部)', '* (all)') : act}</span>
                </label>
              ))}
            </div>
          </div>

          <div>
            <Label htmlFor="grant-desc" className="mb-1 block text-xs text-text-muted">{tr('说明（可选）', 'Description (optional)')}</Label>
            <Input
              id="grant-desc"
              value={draft.description}
              onChange={(e) => setDraft((d) => ({ ...d, description: e.target.value }))}
              placeholder={tr('例如：禁止研发人员直接连接生产环境 WebSSH', 'e.g. Block devs from prod WebSSH')}
              className="w-full"
            />
          </div>

          {modalError && (
            <div className="rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-600 dark:text-red-300">
              {modalError}
            </div>
          )}
        </div>
      </Modal>

      {dialog}
    </div>
  );
}
