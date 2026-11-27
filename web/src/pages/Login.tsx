import { Button, Input, Label } from '@/components/ui';
import { QrCode } from '@/components/QrCode';
import { useEffect, useState, type FormEvent } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { ArrowLeft, Check, Copy, Globe, KeyRound, Loader2, ShieldCheck } from 'lucide-react';
import {
  enrollMfaConfirm,
  enrollMfaStart,
  listPublicSsoProviders,
  login,
  verifyMfa,
  type LoginResponse,
  type MfaEnrollStartResp,
  type PublicSsoProvider,
} from '@/api/auth';
import { ApiError } from '@/api/client';
import { OpsPilotLogo } from '@/components/OpsPilotLogo';
import { useI18n } from '@/i18n/locale';
import { useAuth } from '@/store/auth';

type LoginStep = 'credentials' | 'mfa_verify' | 'mfa_enroll' | 'recovery_codes';

type LocationMfaState = {
  from?: string;
  mfa?: LoginResponse;
};

export default function LoginPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const setSession = useAuth((s) => s.setSession);
  const { tr } = useI18n();

  const locState = (location.state ?? {}) as LocationMfaState;
  const fromPath =
    typeof locState.from === 'string' && locState.from.startsWith('/') && !locState.from.startsWith('//') && !locState.from.startsWith('/login')
      ? locState.from
      : '/';

  const [step, setStep] = useState<LoginStep>('credentials');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [mfaToken, setMfaToken] = useState('');
  const [mfaRedirectTo, setMfaRedirectTo] = useState<string>(fromPath);
  const [mfaCode, setMfaCode] = useState('');
  const [enrollData, setEnrollData] = useState<MfaEnrollStartResp | null>(null);
  const [pendingSession, setPendingSession] = useState<{ access_token: string; refresh_token?: string; role: string; email: string } | null>(null);
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([]);
  const [copiedCodes, setCopiedCodes] = useState(false);

  const [ssoProviders, setSsoProviders] = useState<PublicSsoProvider[]>([]);
  const [allowLocalLogin, setAllowLocalLogin] = useState(true);

  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    void listPublicSsoProviders()
      .then((res) => {
        if (cancelled) return;
        setSsoProviders(res.items ?? []);
        setAllowLocalLogin(res.allow_local_password_login !== false);
      })
      .catch(() => {
        // Fallback smoothly when backend does not expose SSO discovery
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    const incoming = locState.mfa;
    if (!incoming?.mfa_token) return;
    setMfaToken(incoming.mfa_token);
    setEmail(incoming.email ?? '');
    if (incoming.redirect_to) setMfaRedirectTo(incoming.redirect_to);
    if (incoming.mfa_setup_required) {
      setStep('mfa_enroll');
      void startForcedEnroll(incoming.mfa_token);
    } else if (incoming.mfa_required) {
      setStep('mfa_verify');
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [locState.mfa?.mfa_token]);

  async function startForcedEnroll(token: string) {
    setPending(true);
    setError(null);
    try {
      const res = await enrollMfaStart(token);
      setEnrollData(res);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : tr('初始化 MFA 绑定失败', 'Failed to initialize MFA enrollment'));
    } finally {
      setPending(false);
    }
  }

  function finishSession(res: LoginResponse, fallbackEmail?: string) {
    if (!res.access_token) return;
    setSession({
      access_token: res.access_token,
      refresh_token: res.refresh_token,
      role: res.user?.role ?? res.role ?? 'user',
      email: res.user?.email ?? res.email ?? fallbackEmail ?? email.trim(),
    });
    const nextPath =
      res.redirect_to && res.redirect_to.startsWith('/') && !res.redirect_to.startsWith('//') && !res.redirect_to.startsWith('/login')
        ? res.redirect_to
        : mfaRedirectTo || '/';
    navigate(nextPath, { replace: true });
  }

  async function onSubmitCredentials(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setPending(true);
    try {
      const res = await login(email.trim(), password);
      if (res.mfa_required && res.mfa_token) {
        setMfaToken(res.mfa_token);
        setMfaCode('');
        setStep('mfa_verify');
        return;
      }
      if (res.mfa_setup_required && res.mfa_token) {
        setMfaToken(res.mfa_token);
        setMfaCode('');
        setStep('mfa_enroll');
        await startForcedEnroll(res.mfa_token);
        return;
      }
      finishSession(res);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setError(tr('邮箱或密码错误', 'Invalid email or password'));
      } else if (err instanceof ApiError && err.status === 429) {
        setError(tr('失败次数过多，请稍后再试', 'Too many failed attempts; please try again later'));
      } else if (err instanceof ApiError) {
        setError(err.message || tr('登录失败，请稍后重试', 'Sign-in failed; please try again'));
      } else {
        setError(tr('登录失败，请稍后重试', 'Sign-in failed; please try again'));
      }
    } finally {
      setPending(false);
    }
  }

  async function onSubmitMfaVerify(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setPending(true);
    try {
      const res = await verifyMfa(mfaToken, mfaCode.trim());
      finishSession(res);
    } catch (err) {
      setError(
        err instanceof ApiError && err.status === 401
          ? tr('验证码或恢复码无效，请重试', 'Invalid verification or recovery code')
          : err instanceof ApiError
            ? err.message
            : tr('MFA 验证失败', 'MFA verification failed'),
      );
    } finally {
      setPending(false);
    }
  }

  async function onSubmitForcedEnroll(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setPending(true);
    try {
      const res = await enrollMfaConfirm(mfaToken, mfaCode.trim());
      if (res.access_token) {
        setPendingSession({
          access_token: res.access_token,
          refresh_token: res.refresh_token,
          role: res.role ?? 'user',
          email: res.email ?? email.trim(),
        });
        setRecoveryCodes(res.recovery_codes ?? []);
        setStep('recovery_codes');
      }
    } catch (err) {
      setError(
        err instanceof ApiError && err.status === 401
          ? tr('动态验证码无效，请核对手机时间并重试', 'Invalid verification code; check authenticator time and retry')
          : err instanceof ApiError
            ? err.message
            : tr('绑定失败', 'Enrollment failed'),
      );
    } finally {
      setPending(false);
    }
  }

  function startSso(slug: string) {
    window.location.assign(`/api/v1/auth/sso/${encodeURIComponent(slug)}/start?redirect=${encodeURIComponent(fromPath)}`);
  }

  async function copyRecoveryCodes() {
    try {
      await navigator.clipboard.writeText(recoveryCodes.join('\n'));
      setCopiedCodes(true);
      window.setTimeout(() => setCopiedCodes(false), 2000);
    } catch {
      // Clipboard may fail in non-HTTPS contexts
    }
  }

  function completeRecoveryStep() {
    if (!pendingSession) return;
    setSession(pendingSession);
    navigate(mfaRedirectTo || '/', { replace: true });
  }

  const ssoTypeLabel = (typ: string) => {
    switch (typ) {
      case 'feishu':
        return tr('飞书', 'Feishu');
      case 'wecom':
        return tr('企业微信', 'WeCom');
      default:
        return 'OIDC';
    }
  };

  return (
    <div className="relative flex min-h-screen items-start justify-center bg-bg px-4 pt-[12vh] text-text">
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 overflow-hidden"
        style={{
          background:
            'radial-gradient(circle at 30% 20%, rgba(140,109,240,0.12), transparent 55%), radial-gradient(circle at 70% 80%, rgba(48,166,208,0.10), transparent 55%)',
        }}
      />

      <div className="relative w-full max-w-sm rounded-2xl border border-border bg-card/95 p-7 shadow-xl backdrop-blur">
        <div className="mb-6 text-center">
          <div className="mx-auto mb-3 flex h-16 w-16 items-center justify-center">
            <OpsPilotLogo size={64} />
          </div>
          <h1 className="text-xl font-semibold text-text">{tr('登录到 OpsPilot', 'Sign in to OpsPilot')}</h1>
          <p className="mt-1 text-xs text-text-muted">{tr('AIOps 智能运维工作台', 'AIOps Workbench')}</p>
        </div>

        {step === 'credentials' && (
          <>
            {(allowLocalLogin || ssoProviders.length === 0) ? (
              <form onSubmit={onSubmitCredentials} className="space-y-3" noValidate>
                <div>
                  <Label htmlFor="email" className="mb-1 block text-xs text-text-muted">
                    {tr('邮箱', 'Email')}
                  </Label>
                  <Input
                    id="email"
                    type="email"
                    required
                    autoComplete="email"
                    autoFocus
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    className="w-full"
                    placeholder="you@example.com"
                  />
                </div>

                <div>
                  <Label htmlFor="password" className="mb-1 block text-xs text-text-muted">
                    {tr('密码', 'Password')}
                  </Label>
                  <Input
                    id="password"
                    type="password"
                    required
                    autoComplete="current-password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    className="w-full"
                    placeholder="••••••••"
                  />
                </div>

                {error && (
                  <div
                    role="alert"
                    className="rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-600 dark:text-red-300"
                  >
                    {error}
                  </div>
                )}

                <Button
                  variant="primary"
                  type="submit"
                  disabled={pending || !email || !password}
                  aria-label={tr('登录', 'Sign in')}
                  className="inline-flex w-full items-center justify-center gap-2 px-3 py-2 font-medium"
                >
                  {pending && <Loader2 size={14} className="animate-spin" />}
                  <span>{tr('登录', 'Sign in')}</span>
                </Button>
              </form>
            ) : (
              <div className="rounded-lg border border-border bg-bg/60 px-3 py-2.5 text-center text-xs text-text-muted">
                {tr('管理员已启用仅企业 SSO 登录策略，请使用下方单点登录入口。', 'Local password sign-in is disabled by policy. Please sign in with enterprise SSO below.')}
              </div>
            )}

            {ssoProviders.length > 0 && (
              <div className="mt-5 space-y-2.5">
                <div className="relative flex items-center py-1">
                  <div className="flex-grow border-t border-border" />
                  <span className="mx-3 shrink-0 text-[11px] text-text-faint">
                    {tr('企业单点登录', 'Enterprise SSO')}
                  </span>
                  <div className="flex-grow border-t border-border" />
                </div>
                <div className="space-y-2">
                  {ssoProviders.map((p) => (
                    <Button
                      key={p.id}
                      variant="outline"
                      type="button"
                      onClick={() => startSso(p.slug)}
                      className="w-full justify-between px-3"
                    >
                      <span className="flex items-center gap-2 truncate">
                        <Globe size={14} className="shrink-0 text-indigo-500" />
                        <span className="truncate">{p.name}</span>
                      </span>
                      <span className="rounded bg-bg px-1.5 py-0.5 text-[10px] font-medium text-text-muted">
                        {ssoTypeLabel(p.type)}
                      </span>
                    </Button>
                  ))}
                </div>
              </div>
            )}
          </>
        )}

        {step === 'mfa_verify' && (
          <form onSubmit={onSubmitMfaVerify} className="space-y-3" noValidate>
            <div className="flex items-center gap-2 rounded-lg border border-border bg-bg/60 px-3 py-2 text-xs text-text-muted">
              <ShieldCheck size={15} className="shrink-0 text-indigo-500" />
              <span className="truncate">
                {tr('请输入认证器中的 6 位动态码或一次性恢复码', 'Enter the 6-digit TOTP code or a single-use recovery code')}
              </span>
            </div>

            <div>
              <Label htmlFor="mfa-code" className="mb-1 block text-xs text-text-muted">
                {tr('动态验证码 / 恢复码', 'Verification / Recovery code')}
              </Label>
              <Input
                id="mfa-code"
                type="text"
                autoFocus
                autoComplete="one-time-code"
                value={mfaCode}
                onChange={(e) => setMfaCode(e.target.value)}
                className="w-full font-mono tracking-wider"
                placeholder="123456 or ABCD-EFGH"
              />
            </div>

            {error && (
              <div
                role="alert"
                className="rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-600 dark:text-red-300"
              >
                {error}
              </div>
            )}

            <Button
              variant="primary"
              type="submit"
              disabled={pending || mfaCode.trim().length < 6}
              className="w-full justify-center gap-2"
            >
              {pending && <Loader2 size={14} className="animate-spin" />}
              <span>{tr('验证并登录', 'Verify & Sign in')}</span>
            </Button>
            <Button
              variant="ghost"
              type="button"
              onClick={() => {
                setStep('credentials');
                setMfaToken('');
                setError(null);
              }}
              className="w-full justify-center gap-1.5 text-text-muted"
            >
              <ArrowLeft size={13} />
              <span>{tr('返回重新登录', 'Back to sign in')}</span>
            </Button>
          </form>
        )}

        {step === 'mfa_enroll' && (
          <form onSubmit={onSubmitForcedEnroll} className="space-y-3" noValidate>
            <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-300">
              {tr('安全策略要求您的账号必须开启 TOTP 双因素认证。请扫码或录入密钥完成绑定。', 'Security policy requires TOTP MFA for your account. Scan the QR code or enter the secret to continue.')}
            </div>

            {enrollData ? (
              <div className="flex flex-col items-center gap-2 py-1">
                <QrCode value={enrollData.otpauth_uri} size={156} label={tr('MFA 绑定二维码', 'MFA enrollment QR code')} />
                <div className="w-full rounded-md border border-border bg-bg px-2.5 py-1.5 text-center font-mono text-[11px] text-text-muted select-all break-all">
                  {enrollData.secret}
                </div>
              </div>
            ) : (
              <div className="flex h-32 items-center justify-center text-xs text-text-muted">
                <Loader2 size={14} className="mr-2 animate-spin" /> {tr('正在生成密钥…', 'Generating secret…')}
              </div>
            )}

            <div>
              <Label htmlFor="enroll-code" className="mb-1 block text-xs text-text-muted">
                {tr('输入 6 位验证码确认绑定', 'Enter 6-digit code to confirm')}
              </Label>
              <Input
                id="enroll-code"
                type="text"
                autoComplete="one-time-code"
                value={mfaCode}
                onChange={(e) => setMfaCode(e.target.value)}
                className="w-full font-mono tracking-widest"
                placeholder="123456"
              />
            </div>

            {error && (
              <div
                role="alert"
                className="rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-600 dark:text-red-300"
              >
                {error}
              </div>
            )}

            <Button
              variant="primary"
              type="submit"
              disabled={pending || !enrollData || mfaCode.trim().length < 6}
              className="w-full justify-center gap-2"
            >
              {pending && <Loader2 size={14} className="animate-spin" />}
              <KeyRound size={14} />
              <span>{tr('完成绑定并进入控制台', 'Enable MFA & Continue')}</span>
            </Button>
          </form>
        )}

        {step === 'recovery_codes' && (
          <div className="space-y-3">
            <div className="rounded-lg border border-emerald-500/30 bg-emerald-500/10 px-3 py-2 text-xs text-emerald-700 dark:text-emerald-300">
              {tr('MFA 已启用！请妥善保存以下一次性恢复码（仅展示这一次），手机丢失时可用于登录。', 'MFA enabled! Save these single-use recovery codes now — they are shown only once.')}
            </div>
            <div className="grid grid-cols-2 gap-1.5 rounded-lg border border-border bg-bg p-3 font-mono text-xs text-text">
              {recoveryCodes.map((c) => (
                <div key={c} className="rounded bg-card px-2 py-1 text-center">{c}</div>
              ))}
            </div>
            <Button variant="outline" type="button" onClick={copyRecoveryCodes} className="w-full justify-center gap-1.5">
              {copiedCodes ? <Check size={13} className="text-emerald-500" /> : <Copy size={13} />}
              <span>{copiedCodes ? tr('已复制全部恢复码', 'Copied recovery codes') : tr('复制恢复码', 'Copy recovery codes')}</span>
            </Button>
            <Button variant="primary" type="button" onClick={completeRecoveryStep} className="w-full justify-center">
              {tr('我已保存，进入 OpsPilot', 'I have saved them, continue')}
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}
