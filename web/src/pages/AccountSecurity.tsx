import { useCallback, useEffect, useState } from 'react';
import {
  Check,
  Copy,
  Globe,
  KeyRound,
  Loader2,
  RefreshCw,
  ShieldCheck,
  ShieldOff,
  Trash2,
} from 'lucide-react';
import { ApiError } from '@/api/client';
import {
  confirmMfa,
  disableMfa,
  getMfaStatus,
  listMyIdentities,
  regenerateRecoveryCodes,
  setupMfa,
  unlinkMyIdentity,
  type LinkedSsoIdentity,
  type MfaSetupResult,
  type MfaStatus,
} from '@/api/security';
import { Modal } from '@/components/Modal';
import { QrCode } from '@/components/QrCode';
import { Button, Card, Chip, EmptyState, Input, Label, PageHeader } from '@/components/ui';
import { useDialogs } from '@/components/ui/useDialogs';
import { useI18n } from '@/i18n/locale';
import { useAssistantPage } from '@/lib/assistantPage';
import { useMe } from '@/store/me';

export default function AccountSecurityPage() {
  const { tr } = useI18n();
  const { me, refresh: refreshMe } = useMe();
  const { confirmAction, dialog } = useDialogs();

  const [status, setStatus] = useState<MfaStatus | null>(null);
  const [identities, setIdentities] = useState<LinkedSsoIdentity[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  // Enroll modal
  const [setupOpen, setSetupOpen] = useState(false);
  const [setupData, setSetupData] = useState<MfaSetupResult | null>(null);
  const [enrollCode, setEnrollCode] = useState('');
  const [enrollSaving, setEnrollSaving] = useState(false);
  const [enrollError, setEnrollError] = useState<string | null>(null);
  const [copiedSecret, setCopiedSecret] = useState(false);

  // Recovery codes modal (shown once after confirm or regenerate)
  const [recoveryCodes, setRecoveryCodes] = useState<string[] | null>(null);
  const [copiedCodes, setCopiedCodes] = useState(false);

  // Disable / Regenerate code prompt modal
  const [codeAction, setCodeAction] = useState<'disable' | 'regen' | null>(null);
  const [actionCode, setActionCode] = useState('');
  const [actionSaving, setActionSaving] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  const loadAll = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [st, idents] = await Promise.all([getMfaStatus(), listMyIdentities()]);
      setStatus(st);
      setIdentities(idents.items ?? []);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : (e as Error).message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadAll();
  }, [loadAll]);

  useAssistantPage('账号安全', 'Account security', {
    mfa_enabled: status?.enabled ?? false,
    linked_sso_count: identities.length,
  });

  async function openSetup() {
    setEnrollError(null);
    setEnrollCode('');
    setSetupData(null);
    setSetupOpen(true);
    try {
      const res = await setupMfa();
      setSetupData(res);
    } catch (e) {
      setEnrollError(e instanceof ApiError ? e.message : (e as Error).message);
    }
  }

  async function handleConfirmMfa() {
    setEnrollSaving(true);
    setEnrollError(null);
    try {
      const res = await confirmMfa(enrollCode.trim());
      setSetupOpen(false);
      setRecoveryCodes(res.recovery_codes ?? []);
      setNotice(tr('TOTP 双因素认证已启用', 'TOTP MFA has been enabled'));
      await loadAll();
      await refreshMe();
    } catch (e) {
      setEnrollError(e instanceof ApiError ? e.message : (e as Error).message);
    } finally {
      setEnrollSaving(false);
    }
  }

  async function handleCodeAction() {
    if (!codeAction) return;
    setActionSaving(true);
    setActionError(null);
    try {
      if (codeAction === 'disable') {
        await disableMfa(actionCode.trim());
        setCodeAction(null);
        setActionCode('');
        setNotice(tr('已停用双因素认证', 'TOTP MFA disabled'));
      } else {
        const res = await regenerateRecoveryCodes(actionCode.trim());
        setCodeAction(null);
        setActionCode('');
        setRecoveryCodes(res.recovery_codes ?? []);
        setNotice(tr('已重新生成恢复码，旧恢复码已失效', 'Recovery codes regenerated; previous codes invalidated'));
      }
      await loadAll();
      await refreshMe();
    } catch (e) {
      setActionError(e instanceof ApiError ? e.message : (e as Error).message);
    } finally {
      setActionSaving(false);
    }
  }

  async function handleUnlink(item: LinkedSsoIdentity) {
    const ok = await confirmAction(
      tr(
        `确认解除与 ${item.provider_name || item.provider_slug}（${item.external_email || item.external_sub}）的绑定？解绑后如无其他登录方式需先确认可用密码或重新授权。`,
        `Unlink ${item.provider_name || item.provider_slug} (${item.external_email || item.external_sub}) from your account?`,
      ),
    );
    if (!ok) return;
    try {
      await unlinkMyIdentity(item.id);
      setIdentities((prev) => prev.filter((x) => x.id !== item.id));
      setNotice(tr('已解除 SSO 账号绑定', 'SSO identity unlinked'));
    } catch (e) {
      setError(e instanceof ApiError ? e.message : (e as Error).message);
    }
  }

  async function copyText(text: string, which: 'secret' | 'codes') {
    try {
      await navigator.clipboard.writeText(text);
      if (which === 'secret') {
        setCopiedSecret(true);
        window.setTimeout(() => setCopiedSecret(false), 2000);
      } else {
        setCopiedCodes(true);
        window.setTimeout(() => setCopiedCodes(false), 2000);
      }
    } catch {
      // Clipboard blocked
    }
  }

  return (
    <main className="anim-fade flex-1 overflow-y-auto p-6">
      <div className="mx-auto max-w-4xl space-y-5">
        <PageHeader
          title={tr('账号安全', 'Account security')}
          subtitle={
            me?.email
              ? tr(`${me.email} — 多因素认证与外部 SSO 绑定`, `${me.email} — multi-factor authentication and linked SSO identities`)
              : tr('管理个人双因素认证与已绑定单点登录身份', 'Manage personal MFA and linked SSO identities')
          }
          className="border-0 px-0 py-0"
          actions={
            <Button onClick={() => void loadAll()} disabled={loading}>
              {loading ? <Loader2 size={13} className="animate-spin" /> : <RefreshCw size={13} />}
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

        <Card className="space-y-4 p-5">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="min-w-0 space-y-1">
              <div className="flex items-center gap-2">
                <ShieldCheck size={16} className="text-indigo-500" />
                <h2 className="text-sm font-semibold text-text">
                  {tr('TOTP 双因素认证', 'TOTP Two-Factor Authentication')}
                </h2>
                <Chip tone={status?.enabled ? 'success' : 'default'}>
                  {status?.enabled ? tr('已启用', 'Enabled') : tr('未启用', 'Disabled')}
                </Chip>
                {status?.enforced && (
                  <Chip tone="warning">{tr('策略强制要求', 'Required by policy')}</Chip>
                )}
              </div>
              <p className="text-xs text-text-muted">
                {tr(
                  '使用 Google Authenticator、1Password、飞书或企业微信令牌生成 6 位动态验证码，保护您的控制台登录。',
                  'Use an authenticator app to generate 6-digit verification codes when signing in.',
                )}
              </p>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              {!status?.enabled ? (
                <Button variant="primary" onClick={() => void openSetup()} disabled={loading}>
                  <KeyRound size={13} />
                  <span>{tr('开启 TOTP MFA', 'Enable TOTP MFA')}</span>
                </Button>
              ) : (
                <>
                  <Button
                    variant="outline"
                    onClick={() => {
                      setActionCode('');
                      setActionError(null);
                      setCodeAction('regen');
                    }}
                  >
                    <RefreshCw size={13} />
                    <span>{tr('重新生成恢复码', 'Regenerate recovery codes')}</span>
                  </Button>
                  <Button
                    variant="dangerGhost"
                    disabled={Boolean(status?.enforced)}
                    title={status?.enforced ? tr('当前平台安全策略强制启用 MFA，无法自行停用', 'Mandatory under current auth policy') : undefined}
                    onClick={() => {
                      setActionCode('');
                      setActionError(null);
                      setCodeAction('disable');
                    }}
                  >
                    <ShieldOff size={13} />
                    <span>{tr('停用 MFA', 'Disable MFA')}</span>
                  </Button>
                </>
              )}
            </div>
          </div>

          {status?.enabled && (
            <div className="grid gap-3 rounded-lg border border-border bg-bg/50 p-3.5 text-xs sm:grid-cols-2">
              <div>
                <div className="text-text-muted">{tr('剩余一次性恢复码', 'Remaining recovery codes')}</div>
                <div className="mt-0.5 font-mono text-sm font-semibold text-text">
                  {status.recovery_codes_remaining} / 8
                </div>
              </div>
              <div>
                <div className="text-text-muted">{tr('启用时间', 'Enabled at')}</div>
                <div className="mt-0.5 font-mono text-text">
                  {status.verified_at ? new Date(status.verified_at).toLocaleString() : '—'}
                </div>
              </div>
            </div>
          )}
        </Card>

        <Card className="p-0">
          <div className="flex items-center justify-between border-b border-border px-5 py-3.5">
            <div className="flex items-center gap-2">
              <Globe size={15} className="text-indigo-500" />
              <h2 className="text-sm font-semibold text-text">
                {tr('已绑定的企业 SSO 身份', 'Linked Enterprise SSO Identities')}
              </h2>
            </div>
            <span className="text-xs text-text-muted">
              {tr(`${identities.length} 个关联账号`, `${identities.length} linked`)}
            </span>
          </div>

          {identities.length === 0 ? (
            <EmptyState
              icon={Globe}
              title={tr('尚未绑定企业 SSO 身份', 'No linked enterprise SSO identities')}
              hint={tr('下次通过 OIDC / 飞书 / 企业微信登录 OpsPilot 时会自动与同邮箱账号关联。', 'Signing in via OIDC, Feishu, or WeCom will automatically link matching accounts.')}
              className="flex h-44 flex-col items-center justify-center gap-1.5 text-center"
            />
          ) : (
            <div className="overflow-x-auto">
              <table className="og-resource-table">
                <thead>
                  <tr>
                    <th>{tr('登录提供商', 'Provider')}</th>
                    <th>{tr('外部邮箱 / 标识', 'External account')}</th>
                    <th>{tr('最近登录', 'Last sign-in')}</th>
                    <th className="text-left">{tr('操作', 'Actions')}</th>
                  </tr>
                </thead>
                <tbody>
                  {identities.map((item) => (
                    <tr key={item.id}>
                      <td>
                        <div className="flex items-center gap-2">
                          <span className="font-medium text-text">{item.provider_name || item.provider_slug}</span>
                          <Chip dense tone="info">{item.provider_type.toUpperCase()}</Chip>
                        </div>
                      </td>
                      <td>
                        <div className="text-text">{item.external_email || item.external_name || '—'}</div>
                        <div className="font-mono text-[11px] text-text-faint">{item.external_sub}</div>
                      </td>
                      <td className="text-xs text-text-muted">
                        {item.last_login_at ? new Date(item.last_login_at).toLocaleString() : '—'}
                      </td>
                      <td>
                        <Button size="sm" variant="dangerGhost" onClick={() => void handleUnlink(item)}>
                          <Trash2 size={12} />
                          <span>{tr('解绑', 'Unlink')}</span>
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      </div>

      {/* Enroll Modal */}
      <Modal
        open={setupOpen}
        onClose={() => setSetupOpen(false)}
        title={tr('开启 TOTP 双因素认证', 'Enable TOTP Two-Factor Authentication')}
        size="md"
        footer={
          <>
            <Button onClick={() => setSetupOpen(false)}>{tr('取消', 'Cancel')}</Button>
            <Button
              variant="primary"
              disabled={enrollSaving || !setupData || enrollCode.trim().length < 6}
              onClick={() => void handleConfirmMfa()}
            >
              {enrollSaving && <Loader2 size={13} className="animate-spin" />}
              <span>{tr('验证并启用', 'Verify & Enable')}</span>
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          {!setupData && !enrollError ? (
            <div className="flex h-36 items-center justify-center text-xs text-text-muted">
              <Loader2 size={14} className="mr-2 animate-spin" /> {tr('正在生成密钥…', 'Generating TOTP secret…')}
            </div>
          ) : setupData ? (
            <>
              <p className="text-xs text-text-muted">
                {tr(
                  '1. 使用手机认证器扫描下方二维码，或手动复制密钥添加账户。',
                  '1. Scan this QR code in your authenticator app or enter the secret manually.',
                )}
              </p>
              <div className="flex flex-col items-center gap-3 py-1">
                <QrCode value={setupData.otpauth_uri} size={176} label={tr('TOTP 绑定二维码', 'TOTP setup QR code')} />
                <div className="flex w-full items-center gap-2 rounded-lg border border-border bg-bg px-3 py-2">
                  <code className="min-w-0 flex-1 break-all font-mono text-xs text-text">{setupData.secret}</code>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => void copyText(setupData.secret, 'secret')}
                    aria-label={tr('复制密钥', 'Copy secret')}
                  >
                    {copiedSecret ? <Check size={12} className="text-emerald-500" /> : <Copy size={12} />}
                    <span>{copiedSecret ? tr('已复制', 'Copied') : tr('复制', 'Copy')}</span>
                  </Button>
                </div>
              </div>
              <div>
                <Label htmlFor="setup-confirm-code" className="mb-1 block text-xs text-text-muted">
                  {tr('2. 输入认证器生成的 6 位验证码确认绑定', '2. Enter the 6-digit verification code to confirm')}
                </Label>
                <Input
                  id="setup-confirm-code"
                  type="text"
                  autoComplete="one-time-code"
                  value={enrollCode}
                  onChange={(e) => setEnrollCode(e.target.value)}
                  className="w-full font-mono tracking-widest"
                  placeholder="123456"
                />
              </div>
            </>
          ) : null}

          {enrollError && (
            <div className="rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-600 dark:text-red-300">
              {enrollError}
            </div>
          )}
        </div>
      </Modal>

      {/* Recovery Codes Modal */}
      <Modal
        open={!!recoveryCodes}
        onClose={() => setRecoveryCodes(null)}
        title={tr('请保存一次性恢复码', 'Save Single-Use Recovery Codes')}
        size="sm"
        footer={
          <>
            <Button
              variant="outline"
              onClick={() => recoveryCodes && void copyText(recoveryCodes.join('\n'), 'codes')}
            >
              {copiedCodes ? <Check size={12} className="text-emerald-500" /> : <Copy size={12} />}
              <span>{copiedCodes ? tr('已复制', 'Copied') : tr('全部复制', 'Copy all')}</span>
            </Button>
            <Button variant="primary" onClick={() => setRecoveryCodes(null)}>
              {tr('我已妥善保存', 'I have saved them')}
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <p className="text-xs text-text-muted">
            {tr(
              '每个恢复码仅可使用一次，本页面关闭后将不再显示完整明文。',
              'Each recovery code can be used once and will not be shown again after closing this dialog.',
            )}
          </p>
          <div className="grid grid-cols-2 gap-2 rounded-lg border border-border bg-bg p-3 font-mono text-xs text-text">
            {(recoveryCodes ?? []).map((code) => (
              <div key={code} className="rounded border border-border/60 bg-card px-2 py-1.5 text-center">
                {code}
              </div>
            ))}
          </div>
        </div>
      </Modal>

      {/* Verify code modal for Disable / Regenerate */}
      <Modal
        open={!!codeAction}
        onClose={() => setCodeAction(null)}
        title={
          codeAction === 'disable'
            ? tr('确认停用双因素认证', 'Confirm Disabling MFA')
            : tr('重新生成恢复码', 'Regenerate Recovery Codes')
        }
        size="sm"
        footer={
          <>
            <Button onClick={() => setCodeAction(null)}>{tr('取消', 'Cancel')}</Button>
            <Button
              variant={codeAction === 'disable' ? 'danger' : 'primary'}
              disabled={actionSaving || actionCode.trim().length < 6}
              onClick={() => void handleCodeAction()}
            >
              {actionSaving && <Loader2 size={13} className="animate-spin" />}
              <span>{codeAction === 'disable' ? tr('确认停用', 'Disable MFA') : tr('确认重新生成', 'Regenerate')}</span>
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <p className="text-xs text-text-muted">
            {codeAction === 'disable'
              ? tr('请输入当前 6 位动态验证码或恢复码以停用 MFA。', 'Enter a current 6-digit TOTP code or recovery code to disable MFA.')
              : tr('请输入当前认证器中的 6 位动态验证码。新恢复码生成后旧恢复码立即失效。', 'Enter a current 6-digit TOTP code. Existing recovery codes will be invalidated.')}
          </p>
          <div>
            <Label htmlFor="mfa-action-code" className="mb-1 block text-xs text-text-muted">
              {tr('验证码', 'Verification code')}
            </Label>
            <Input
              id="mfa-action-code"
              type="text"
              autoFocus
              value={actionCode}
              onChange={(e) => setActionCode(e.target.value)}
              className="w-full font-mono tracking-wider"
              placeholder={codeAction === 'disable' ? '123456 or ABCD-EFGH' : '123456'}
            />
          </div>
          {actionError && (
            <div className="rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-600 dark:text-red-300">
              {actionError}
            </div>
          )}
        </div>
      </Modal>

      {dialog}
    </main>
  );
}
