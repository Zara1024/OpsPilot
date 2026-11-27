import { useEffect, useState } from 'react';
import { useLocation, useNavigate, useParams } from 'react-router-dom';
import { AlertCircle, Loader2 } from 'lucide-react';
import { exchangeSsoCode } from '@/api/auth';
import { ApiError } from '@/api/client';
import { OpsPilotLogo } from '@/components/OpsPilotLogo';
import { Button, Card } from '@/components/ui';
import { useI18n } from '@/i18n/locale';
import { useAuth } from '@/store/auth';

export default function SSOCallbackPage() {
  const { slug: routeSlug } = useParams<{ slug?: string }>();
  const location = useLocation();
  const navigate = useNavigate();
  const setSession = useAuth((s) => s.setSession);
  const { tr } = useI18n();
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const params = new URLSearchParams(location.search);
    const slug = (routeSlug ?? params.get('provider') ?? '').trim();
    const errParam = params.get('error');
    const errDesc = params.get('error_description');
    if (errParam) {
      setError(errDesc ? `${errParam}: ${errDesc}` : errParam);
      return;
    }
    const code = params.get('code') ?? '';
    const state = params.get('state') ?? '';
    if (!slug || !code || !state) {
      setError(tr('SSO 回调参数缺失，请重新发起登录。', 'Missing SSO callback parameters; please try signing in again.'));
      return;
    }
    const via = params.get('via');
    const origin = window.location.origin;
    const redirectUri = via === 'api'
      ? `${origin}/api/v1/auth/sso/${encodeURIComponent(slug)}/callback`
      : `${origin}/login/sso/callback/${encodeURIComponent(slug)}`;

    void exchangeSsoCode(slug, code, state, redirectUri)
      .then((res) => {
        if (cancelled) return;
        const safeTarget =
          res.redirect_to && res.redirect_to.startsWith('/') && !res.redirect_to.startsWith('//') && !res.redirect_to.startsWith('/login')
            ? res.redirect_to
            : '/';
        if (res.mfa_required || res.mfa_setup_required) {
          navigate('/login', {
            replace: true,
            state: { mfa: res, from: safeTarget },
          });
          return;
        }
        if (res.access_token) {
          setSession({
            access_token: res.access_token,
            refresh_token: res.refresh_token,
            role: res.role ?? 'user',
            email: res.email ?? '',
          });
          navigate(safeTarget, { replace: true });
          return;
        }
        setError(tr('SSO 返回结果不完整，请重试。', 'Incomplete SSO login response; please retry.'));
      })
      .catch((err) => {
        if (cancelled) return;
        if (err instanceof ApiError) {
          setError(err.message || tr('SSO 登录失败', 'SSO sign-in failed'));
        } else {
          setError((err as Error).message || tr('SSO 登录失败', 'SSO sign-in failed'));
        }
      });

    return () => {
      cancelled = true;
    };
  }, [location.search, navigate, routeSlug, setSession, tr]);

  return (
    <div className="flex min-h-screen items-center justify-center bg-bg px-4">
      <Card className="w-full max-w-sm space-y-4 p-7 text-center">
        <div className="mx-auto flex h-14 w-14 items-center justify-center">
          <OpsPilotLogo size={52} />
        </div>
        {error ? (
          <>
            <div className="flex items-center justify-center gap-2 text-sm font-medium text-red-500">
              <AlertCircle size={16} />
              <span>{tr('企业单点登录失败', 'Enterprise SSO failed')}</span>
            </div>
            <p className="break-words text-xs text-text-muted">{error}</p>
            <Button variant="primary" className="w-full justify-center" onClick={() => navigate('/login', { replace: true })}>
              {tr('返回登录页', 'Back to sign in')}
            </Button>
          </>
        ) : (
          <div className="flex flex-col items-center gap-2 py-2">
            <Loader2 size={20} className="animate-spin text-indigo-500" />
            <div className="text-sm font-medium text-text">{tr('正在验证企业 SSO 身份…', 'Verifying enterprise SSO identity…')}</div>
            <div className="text-xs text-text-muted">{tr('请稍候，即将进入工作台', 'Please wait, redirecting to workbench')}</div>
          </div>
        )}
      </Card>
    </div>
  );
}
