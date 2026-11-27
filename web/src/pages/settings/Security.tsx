import { useCallback, useEffect, useState } from 'react';
import {
  Check,
  Copy,
  Globe,
  KeyRound,
  Loader2,
  Pencil,
  Plus,
  RefreshCw,
  Save,
  Shield,
  Trash2,
} from 'lucide-react';
import { ApiError } from '@/api/client';
import { listOrgs, type Org } from '@/api/orgs';
import {
  createSsoProvider,
  deleteSsoProvider,
  getAuthPolicy,
  listSsoProviders,
  updateAuthPolicy,
  updateSsoProvider,
  type AuthPolicy,
  type MfaEnforceMode,
  type SsoProvider,
  type SsoProviderInput,
  type SsoProviderType,
} from '@/api/security';
import { Modal } from '@/components/Modal';
import { Button, Card, Chip, EmptyState, Input, Label, PageHeader, Select, Switch } from '@/components/ui';
import { useDialogs } from '@/components/ui/useDialogs';
import { useI18n } from '@/i18n/locale';
import { useAssistantPage } from '@/lib/assistantPage';
import { usePermissions } from '@/store/me';

const DEFAULT_POLICY: AuthPolicy = {
  mfa_enforce_mode: 'off',
  allow_local_password_login: true,
  login_ip_limit: 20,
  login_ip_window_sec: 300,
  login_email_limit: 6,
  login_email_window_sec: 900,
};

function emptyProviderDraft(): SsoProviderInput {
  return {
    slug: '',
    name: '',
    type: 'oidc',
    enabled: true,
    issuer_url: '',
    auth_url: '',
    token_url: '',
    userinfo_url: '',
    jwks_url: '',
    client_id: '',
    client_secret: '',
    corp_id: '',
    agent_id: '',
    scopes: '',
    auto_create_user: true,
    default_role: 'user',
    default_org_id: null,
    allowed_domains: '',
  };
}

export default function AdminSecurityPage() {
  const { tr } = useI18n();
  const { isAdmin } = usePermissions();
  const { confirmAction, dialog } = useDialogs();

  const [policy, setPolicy] = useState<AuthPolicy>(DEFAULT_POLICY);
  const [providers, setProviders] = useState<SsoProvider[]>([]);
  const [orgs, setOrgs] = useState<Org[]>([]);
  const [loading, setLoading] = useState(false);
  const [policySaving, setPolicySaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [copiedSlug, setCopiedSlug] = useState<string | null>(null);

  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<SsoProvider | null>(null);
  const [draft, setDraft] = useState<SsoProviderInput>(emptyProviderDraft);
  const [modalSaving, setModalSaving] = useState(false);
  const [modalError, setModalError] = useState<string | null>(null);

  const loadAll = useCallback(async () => {
    if (!isAdmin) return;
    setLoading(true);
    setError(null);
    try {
      const [pol, ssoResp, orgResp] = await Promise.all([
        getAuthPolicy(),
        listSsoProviders(),
        listOrgs(),
      ]);
      setPolicy(pol);
      setProviders(ssoResp.items ?? []);
      setOrgs(orgResp.items ?? []);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : (e as Error).message);
    } finally {
      setLoading(false);
    }
  }, [isAdmin]);

  useEffect(() => {
    void loadAll();
  }, [loadAll]);

  useAssistantPage('登录与安全', 'Authentication & Security', {
    sso_count: providers.length,
    mfa_enforce_mode: policy.mfa_enforce_mode,
  });

  async function savePolicy() {
    setPolicySaving(true);
    setError(null);
    setNotice(null);
    try {
      const saved = await updateAuthPolicy(policy);
      setPolicy(saved);
      setNotice(tr('安全策略已保存并全局生效', 'Authentication policy saved'));
    } catch (e) {
      setError(e instanceof ApiError ? e.message : (e as Error).message);
    } finally {
      setPolicySaving(false);
    }
  }

  function openCreateModal() {
    setEditing(null);
    setDraft(emptyProviderDraft());
    setModalError(null);
    setModalOpen(true);
  }

  function openEditModal(p: SsoProvider) {
    setEditing(p);
    setDraft({
      slug: p.slug,
      name: p.name,
      type: p.type,
      enabled: p.enabled,
      issuer_url: p.issuer_url ?? '',
      auth_url: p.auth_url ?? '',
      token_url: p.token_url ?? '',
      userinfo_url: p.userinfo_url ?? '',
      jwks_url: p.jwks_url ?? '',
      client_id: p.client_id ?? '',
      client_secret: '',
      corp_id: p.corp_id ?? '',
      agent_id: p.agent_id ?? '',
      scopes: p.scopes ?? '',
      auto_create_user: p.auto_create_user,
      default_role: p.default_role ?? 'user',
      default_org_id: p.default_org_id ?? null,
      allowed_domains: p.allowed_domains ?? '',
    });
    setModalError(null);
    setModalOpen(true);
  }

  async function submitModal() {
    setModalSaving(true);
    setModalError(null);
    try {
      if (editing) {
        const updated = await updateSsoProvider(editing.id, draft);
        setProviders((cur) => cur.map((x) => (x.id === updated.id ? updated : x)));
        setNotice(tr(`已更新 SSO 提供商「${updated.name}」`, `Updated SSO provider "${updated.name}"`));
      } else {
        const created = await createSsoProvider(draft);
        setProviders((cur) => [...cur, created]);
        setNotice(tr(`已添加 SSO 提供商「${created.name}」`, `Added SSO provider "${created.name}"`));
      }
      setModalOpen(false);
    } catch (e) {
      setModalError(e instanceof ApiError ? e.message : (e as Error).message);
    } finally {
      setModalSaving(false);
    }
  }

  async function toggleEnabled(p: SsoProvider) {
    try {
      const updated = await updateSsoProvider(p.id, {
        slug: p.slug,
        name: p.name,
        type: p.type,
        enabled: !p.enabled,
        issuer_url: p.issuer_url ?? '',
        auth_url: p.auth_url ?? '',
        token_url: p.token_url ?? '',
        userinfo_url: p.userinfo_url ?? '',
        jwks_url: p.jwks_url ?? '',
        client_id: p.client_id,
        corp_id: p.corp_id ?? '',
        agent_id: p.agent_id ?? '',
        scopes: p.scopes ?? '',
        auto_create_user: p.auto_create_user,
        default_role: p.default_role,
        default_org_id: p.default_org_id ?? null,
        allowed_domains: p.allowed_domains ?? '',
      });
      setProviders((cur) => cur.map((x) => (x.id === updated.id ? updated : x)));
    } catch (e) {
      setError(e instanceof ApiError ? e.message : (e as Error).message);
    }
  }

  async function removeProvider(p: SsoProvider) {
    const ok = await confirmAction(
      tr(
        `确认删除 SSO 提供商「${p.name}」(${p.slug})？关联的外部 SSO 绑定关系会一并删除（用户本地账号保留）。`,
        `Delete SSO provider "${p.name}" (${p.slug})? Linked SSO bindings will be removed while local user accounts remain.`,
      ),
    );
    if (!ok) return;
    try {
      await deleteSsoProvider(p.id);
      setProviders((cur) => cur.filter((x) => x.id !== p.id));
      setNotice(tr(`已删除「${p.name}」`, `Deleted "${p.name}"`));
    } catch (e) {
      setError(e instanceof ApiError ? e.message : (e as Error).message);
    }
  }

  const callbackUrlFor = (slug: string) => {
    const origin = typeof window !== 'undefined' ? window.location.origin : '';
    const clean = slug.trim().toLowerCase() || '<slug>';
    return `${origin}/login/sso/callback/${clean}`;
  };

  async function copyCallback(slug: string) {
    try {
      await navigator.clipboard.writeText(callbackUrlFor(slug));
      setCopiedSlug(slug);
      window.setTimeout(() => setCopiedSlug(null), 2000);
    } catch {
      // Clipboard blocked
    }
  }

  if (!isAdmin) {
    return (
      <div className="p-6">
        <Card className="p-6">
          <EmptyState
            icon={Shield}
            title={tr('需要管理员权限', 'Admin permission required')}
            hint={tr('只有系统管理员才能配置企业 SSO 与认证安全策略。', 'Only system admins can configure SSO and authentication security.')}
          />
        </Card>
      </div>
    );
  }

  return (
    <div className="space-y-5 p-6">
      <PageHeader
        title={tr('登录与安全', 'Authentication & Security')}
        subtitle={tr('配置多因素认证策略、登录防爆破与企业单点登录（OIDC / 飞书 / 企业微信）', 'Configure MFA policy, shared login throttling, and enterprise SSO')}
        className="border-0 px-0 py-0"
        actions={
          <Button onClick={() => void loadAll()} disabled={loading}>
            {loading ? <Loader2 size={12} className="animate-spin" /> : <RefreshCw size={12} />}
            <span>{tr('刷新', 'Refresh')}</span>
          </Button>
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

      {/* Global Auth Policy */}
      <Card className="space-y-4 p-5">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border pb-3">
          <div className="flex items-center gap-2">
            <KeyRound size={15} className="text-indigo-500" />
            <div>
              <h2 className="text-sm font-semibold text-text">{tr('全局登录与 MFA 策略', 'Global Sign-in & MFA Policy')}</h2>
              <p className="text-xs text-text-muted">
                {tr('限流计数基于数据库在多副本之间共享', 'Failed login throttles are shared across control-plane replicas in DB')}
              </p>
            </div>
          </div>
          <Button variant="primary" onClick={() => void savePolicy()} disabled={policySaving}>
            {policySaving ? <Loader2 size={13} className="animate-spin" /> : <Save size={13} />}
            <span>{tr('保存策略', 'Save policy')}</span>
          </Button>
        </div>

        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <div className="space-y-1.5">
            <Label htmlFor="mfa-enforce" className="text-xs text-text-muted">
              {tr('MFA 强制范围', 'MFA enforcement')}
            </Label>
            <Select
              id="mfa-enforce"
              aria-label={tr('MFA 强制范围', 'MFA enforcement')}
              value={policy.mfa_enforce_mode}
              onValueChange={(v) => setPolicy((p) => ({ ...p, mfa_enforce_mode: v as MfaEnforceMode }))}
              options={[
                { value: 'off', label: tr('可选（用户自助开启）', 'Optional (self-service)') },
                { value: 'admins', label: tr('仅强制管理员开启 MFA', 'Admins required') },
                { value: 'all', label: tr('全员强制开启 MFA', 'All users required') },
              ]}
              className="w-full"
            />
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs text-text-muted">
              {tr('本地邮箱密码登录', 'Local password login')}
            </Label>
            <div className="flex h-9 items-center justify-between rounded-md border border-border bg-bg/50 px-3">
              <span className="text-xs text-text">
                {policy.allow_local_password_login ? tr('允许本地密码', 'Enabled') : tr('仅允许企业 SSO', 'SSO only')}
              </span>
              <Switch
                aria-label={tr('允许本地邮箱密码登录', 'Allow local password login')}
                checked={policy.allow_local_password_login}
                onCheckedChange={(checked) => setPolicy((p) => ({ ...p, allow_local_password_login: checked }))}
              />
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="email-limit" className="text-xs text-text-muted">
              {tr('单账号失败锁定阈值 / 窗口（秒）', 'Account failure limit / window (s)')}
            </Label>
            <div className="flex items-center gap-2">
              <Input
                id="email-limit"
                type="number"
                min={3}
                max={100}
                value={policy.login_email_limit}
                onChange={(e) => setPolicy((p) => ({ ...p, login_email_limit: Number(e.target.value) || 6 }))}
                className="w-24"
              />
              <span className="text-xs text-text-faint">/</span>
              <Input
                aria-label={tr('单账号锁定窗口秒数', 'Account lockout window seconds')}
                type="number"
                min={30}
                max={86400}
                value={policy.login_email_window_sec}
                onChange={(e) => setPolicy((p) => ({ ...p, login_email_window_sec: Number(e.target.value) || 900 }))}
                className="w-28"
              />
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="ip-limit" className="text-xs text-text-muted">
              {tr('来源 IP 失败限流阈值 / 窗口（秒）', 'Source IP limit / window (s)')}
            </Label>
            <div className="flex items-center gap-2">
              <Input
                id="ip-limit"
                type="number"
                min={3}
                max={1000}
                value={policy.login_ip_limit}
                onChange={(e) => setPolicy((p) => ({ ...p, login_ip_limit: Number(e.target.value) || 20 }))}
                className="w-24"
              />
              <span className="text-xs text-text-faint">/</span>
              <Input
                aria-label={tr('来源 IP 限流窗口秒数', 'Source IP window seconds')}
                type="number"
                min={30}
                max={86400}
                value={policy.login_ip_window_sec}
                onChange={(e) => setPolicy((p) => ({ ...p, login_ip_window_sec: Number(e.target.value) || 300 }))}
                className="w-28"
              />
            </div>
          </div>
        </div>
      </Card>

      {/* Enterprise SSO Providers */}
      <Card className="p-0">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-5 py-3.5">
          <div className="flex items-center gap-2">
            <Globe size={15} className="text-indigo-500" />
            <div>
              <h2 className="text-sm font-semibold text-text">{tr('企业单点登录（SSO）提供商', 'Enterprise SSO Providers')}</h2>
              <p className="text-xs text-text-muted">
                {tr('支持标准 OIDC（Keycloak / Okta / Authing / Azure AD）、飞书与企业微信', 'Supports OIDC, Feishu/Lark, and WeCom with JIT provisioning')}
              </p>
            </div>
          </div>
          <Button variant="primary" onClick={openCreateModal}>
            <Plus size={13} />
            <span>{tr('添加 SSO 提供商', 'Add SSO provider')}</span>
          </Button>
        </div>

        {providers.length === 0 ? (
          <EmptyState
            icon={Globe}
            title={tr('尚未配置企业 SSO 提供商', 'No enterprise SSO providers configured')}
            hint={tr('配置后，登录页会自动呈现企业单点登录入口，并支持自动创建成员加入默认组织。', 'Configured providers appear on the sign-in screen and support JIT user onboarding.')}
            action={
              <Button variant="primary" onClick={openCreateModal}>
                <Plus size={13} />
                <span>{tr('配置第一个 SSO', 'Configure SSO')}</span>
              </Button>
            }
          />
        ) : (
          <div className="overflow-x-auto">
            <table className="og-resource-table">
              <thead>
                <tr>
                  <th>{tr('名称 / 标识', 'Name / Slug')}</th>
                  <th>{tr('协议', 'Protocol')}</th>
                  <th>{tr('自动建号与默认角色', 'JIT & Default role')}</th>
                  <th>{tr('回调地址', 'Redirect URI')}</th>
                  <th>{tr('状态', 'Status')}</th>
                  <th>{tr('操作', 'Actions')}</th>
                </tr>
              </thead>
              <tbody>
                {providers.map((p) => (
                  <tr key={p.id}>
                    <td>
                      <div className="font-medium text-text">{p.name}</div>
                      <div className="font-mono text-[11px] text-text-faint">{p.slug}</div>
                    </td>
                    <td>
                      <Chip tone="info">{p.type.toUpperCase()}</Chip>
                    </td>
                    <td>
                      <div className="flex flex-wrap items-center gap-1.5 text-xs">
                        <Chip tone={p.auto_create_user ? 'success' : 'default'}>
                          {p.auto_create_user ? tr('自动建号', 'JIT on') : tr('仅限已有邮箱', 'Existing only')}
                        </Chip>
                        <Chip tone="default">{p.default_role}</Chip>
                      </div>
                    </td>
                    <td>
                      <div className="flex max-w-[260px] items-center gap-1.5">
                        <code className="truncate font-mono text-[11px] text-text-muted">{callbackUrlFor(p.slug)}</code>
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => void copyCallback(p.slug)}
                          aria-label={tr(`复制 ${p.slug} 回调地址`, `Copy callback URL for ${p.slug}`)}
                        >
                          {copiedSlug === p.slug ? <Check size={12} className="text-emerald-500" /> : <Copy size={12} />}
                        </Button>
                      </div>
                    </td>
                    <td>
                      <Switch
                        aria-label={tr(`切换 ${p.name} 启用状态`, `Toggle ${p.name}`)}
                        checked={p.enabled}
                        onCheckedChange={() => void toggleEnabled(p)}
                      />
                    </td>
                    <td>
                      <div className="flex items-center gap-1.5">
                        <Button size="sm" variant="outline" onClick={() => openEditModal(p)}>
                          <Pencil size={12} />
                          <span>{tr('编辑', 'Edit')}</span>
                        </Button>
                        <Button size="sm" variant="dangerGhost" onClick={() => void removeProvider(p)}>
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

      {/* SSO Create / Edit Modal */}
      <Modal
        open={modalOpen}
        onClose={() => setModalOpen(false)}
        title={editing ? tr(`编辑 SSO 提供商：${editing.name}`, `Edit SSO Provider: ${editing.name}`) : tr('新建企业 SSO 提供商', 'New Enterprise SSO Provider')}
        size="lg"
        footer={
          <>
            <Button onClick={() => setModalOpen(false)}>{tr('取消', 'Cancel')}</Button>
            <Button variant="primary" onClick={() => void submitModal()} disabled={modalSaving}>
              {modalSaving && <Loader2 size={13} className="animate-spin" />}
              <span>{editing ? tr('保存更改', 'Save changes') : tr('创建提供商', 'Create provider')}</span>
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <div>
              <Label htmlFor="sso-name" className="mb-1 block text-xs text-text-muted">{tr('显示名称', 'Display name')}</Label>
              <Input id="sso-name" value={draft.name} onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))} placeholder={tr('例如：公司 Keycloak', 'e.g. Corp Okta')} className="w-full" />
            </div>
            <div>
              <Label htmlFor="sso-slug" className="mb-1 block text-xs text-text-muted">{tr('唯一 Slug 标识', 'Slug')}</Label>
              <Input id="sso-slug" value={draft.slug} onChange={(e) => setDraft((d) => ({ ...d, slug: e.target.value }))} placeholder="corp-oidc" className="w-full font-mono" />
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <div>
              <Label htmlFor="sso-type" className="mb-1 block text-xs text-text-muted">{tr('协议类型', 'Provider type')}</Label>
              <Select
                id="sso-type"
                value={draft.type}
                onValueChange={(v) => setDraft((d) => ({ ...d, type: v as SsoProviderType }))}
                options={[
                  { value: 'oidc', label: 'OIDC (OpenID Connect)' },
                  { value: 'feishu', label: tr('飞书 (Feishu / Lark)', 'Feishu / Lark') },
                  { value: 'wecom', label: tr('企业微信 (WeCom)', 'WeCom') },
                ]}
                className="w-full"
              />
            </div>
            <div>
              <Label className="mb-1 block text-xs text-text-muted">{tr('回调 URI（复制到 IdP 后台）', 'Redirect URI (paste into IdP)')}</Label>
              <div className="flex h-9 items-center rounded-md border border-border bg-bg/60 px-2.5 font-mono text-[11px] text-text-muted select-all">
                <span className="truncate">{callbackUrlFor(draft.slug)}</span>
              </div>
            </div>
          </div>

          {draft.type === 'oidc' && (
            <div className="space-y-3 rounded-lg border border-border bg-bg/40 p-3">
              <div>
                <Label htmlFor="sso-issuer" className="mb-1 block text-xs text-text-muted">{tr('OIDC Issuer URL（支持 .well-known 自动发现）', 'OIDC Issuer URL (supports discovery)')}</Label>
                <Input id="sso-issuer" value={draft.issuer_url ?? ''} onChange={(e) => setDraft((d) => ({ ...d, issuer_url: e.target.value }))} placeholder="https://idp.company.com/realms/prod" className="w-full font-mono" />
              </div>
              <div className="grid gap-3 sm:grid-cols-2">
                <div>
                  <Label htmlFor="sso-client-id" className="mb-1 block text-xs text-text-muted">Client ID</Label>
                  <Input id="sso-client-id" value={draft.client_id} onChange={(e) => setDraft((d) => ({ ...d, client_id: e.target.value }))} className="w-full font-mono" />
                </div>
                <div>
                  <Label htmlFor="sso-secret" className="mb-1 block text-xs text-text-muted">
                    {editing?.has_client_secret ? tr('Client Secret（留空保持不变）', 'Client Secret (blank to keep)') : 'Client Secret'}
                  </Label>
                  <Input id="sso-secret" type="password" value={draft.client_secret ?? ''} onChange={(e) => setDraft((d) => ({ ...d, client_secret: e.target.value }))} className="w-full font-mono" />
                </div>
              </div>
              <div>
                <Label htmlFor="sso-scopes" className="mb-1 block text-xs text-text-muted">{tr('Scopes（可选，默认 openid email profile）', 'Scopes (optional)')}</Label>
                <Input id="sso-scopes" value={draft.scopes ?? ''} onChange={(e) => setDraft((d) => ({ ...d, scopes: e.target.value }))} placeholder="openid email profile" className="w-full font-mono" />
              </div>
            </div>
          )}

          {draft.type === 'feishu' && (
            <div className="grid gap-3 rounded-lg border border-border bg-bg/40 p-3 sm:grid-cols-2">
              <div>
                <Label htmlFor="fs-appid" className="mb-1 block text-xs text-text-muted">{tr('飞书 App ID', 'Feishu App ID')}</Label>
                <Input id="fs-appid" value={draft.client_id} onChange={(e) => setDraft((d) => ({ ...d, client_id: e.target.value }))} placeholder="cli_xxxxxxxx" className="w-full font-mono" />
              </div>
              <div>
                <Label htmlFor="fs-secret" className="mb-1 block text-xs text-text-muted">
                  {editing?.has_client_secret ? tr('App Secret（留空保持不变）', 'App Secret (blank to keep)') : 'App Secret'}
                </Label>
                <Input id="fs-secret" type="password" value={draft.client_secret ?? ''} onChange={(e) => setDraft((d) => ({ ...d, client_secret: e.target.value }))} className="w-full font-mono" />
              </div>
            </div>
          )}

          {draft.type === 'wecom' && (
            <div className="grid gap-3 rounded-lg border border-border bg-bg/40 p-3 sm:grid-cols-3">
              <div>
                <Label htmlFor="wc-corpid" className="mb-1 block text-xs text-text-muted">{tr('企业 ID (CorpID)', 'Corp ID')}</Label>
                <Input id="wc-corpid" value={draft.corp_id ?? ''} onChange={(e) => setDraft((d) => ({ ...d, corp_id: e.target.value, client_id: e.target.value }))} placeholder="wwxxxxxxxx" className="w-full font-mono" />
              </div>
              <div>
                <Label htmlFor="wc-agentid" className="mb-1 block text-xs text-text-muted">AgentID</Label>
                <Input id="wc-agentid" value={draft.agent_id ?? ''} onChange={(e) => setDraft((d) => ({ ...d, agent_id: e.target.value }))} placeholder="1000002" className="w-full font-mono" />
              </div>
              <div>
                <Label htmlFor="wc-secret" className="mb-1 block text-xs text-text-muted">
                  {editing?.has_client_secret ? tr('应用 Secret（留空不变）', 'App Secret (blank to keep)') : 'App Secret'}
                </Label>
                <Input id="wc-secret" type="password" value={draft.client_secret ?? ''} onChange={(e) => setDraft((d) => ({ ...d, client_secret: e.target.value }))} className="w-full font-mono" />
              </div>
            </div>
          )}

          <div className="grid gap-3 sm:grid-cols-3">
            <div>
              <Label className="mb-1 block text-xs text-text-muted">{tr('自动建号 (JIT)', 'Auto-create users')}</Label>
              <div className="flex h-9 items-center justify-between rounded-md border border-border bg-bg/50 px-3">
                <span className="text-xs text-text">{draft.auto_create_user ? tr('开启', 'Enabled') : tr('关闭', 'Disabled')}</span>
                <Switch
                  aria-label={tr('自动创建新用户', 'Auto-create user')}
                  checked={draft.auto_create_user}
                  onCheckedChange={(checked) => setDraft((d) => ({ ...d, auto_create_user: checked }))}
                />
              </div>
            </div>
            <div>
              <Label htmlFor="sso-def-role" className="mb-1 block text-xs text-text-muted">{tr('新用户默认角色', 'Default role')}</Label>
              <Select
                id="sso-def-role"
                value={draft.default_role}
                onValueChange={(v) => setDraft((d) => ({ ...d, default_role: v as 'admin' | 'user' | 'viewer' }))}
                options={[
                  { value: 'user', label: 'user' },
                  { value: 'viewer', label: 'viewer' },
                  { value: 'admin', label: 'admin' },
                ]}
                className="w-full"
              />
            </div>
            <div>
              <Label htmlFor="sso-def-org" className="mb-1 block text-xs text-text-muted">{tr('默认加入组织', 'Default org')}</Label>
              <Select
                id="sso-def-org"
                value={draft.default_org_id ? String(draft.default_org_id) : ''}
                onValueChange={(v) => setDraft((d) => ({ ...d, default_org_id: v ? Number(v) : null }))}
                options={[
                  { value: '', label: tr('平台默认组织', 'Platform default org') },
                  ...orgs.map((o) => ({ value: String(o.id), label: o.name })),
                ]}
                className="w-full"
              />
            </div>
          </div>

          <div>
            <Label htmlFor="sso-domains" className="mb-1 block text-xs text-text-muted">
              {tr('允许登录邮箱域名白名单（逗号分隔，留空不限制）', 'Allowed email domains (comma-separated, optional)')}
            </Label>
            <Input
              id="sso-domains"
              value={draft.allowed_domains ?? ''}
              onChange={(e) => setDraft((d) => ({ ...d, allowed_domains: e.target.value }))}
              placeholder="corp.example.com, ops.example.com"
              className="w-full font-mono"
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
