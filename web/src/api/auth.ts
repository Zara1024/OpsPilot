import { request } from './client';

export type LoginResponse = {
  access_token?: string;
  refresh_token?: string;
  token_type?: string;
  expires_in?: number;
  role?: string;
  email?: string;
  mfa_required?: boolean;
  mfa_setup_required?: boolean;
  mfa_token?: string;
  redirect_to?: string;
  user?: { email?: string; role?: string; id?: string };
};

export type PublicSsoProvider = {
  id: number;
  slug: string;
  name: string;
  type: 'oidc' | 'feishu' | 'wecom' | string;
};

export type PublicSsoProvidersResp = {
  items: PublicSsoProvider[];
  allow_local_password_login: boolean;
};

export type MfaEnrollStartResp = {
  secret: string;
  otpauth_uri: string;
  issuer: string;
};

export type MfaEnrollConfirmResp = LoginResponse & {
  recovery_codes: string[];
};

export type SelfResponse = {
  email: string;
  role: string;
  id?: string;
};

export function login(email: string, password: string) {
  return request<LoginResponse>('POST', '/auth/login', { email, password }, { noAuth: true });
}

export function refresh(refreshToken: string) {
  return request<LoginResponse>('POST', '/auth/refresh', { refresh_token: refreshToken }, { noAuth: true });
}

export function getSelf() {
  return request<SelfResponse>('GET', '/auth/self');
}

export function verifyMfa(mfaToken: string, code: string) {
  return request<LoginResponse>('POST', '/auth/mfa/verify', { mfa_token: mfaToken, code }, { noAuth: true });
}

export function enrollMfaStart(mfaToken: string) {
  return request<MfaEnrollStartResp>('POST', '/auth/mfa/enroll/start', { mfa_token: mfaToken }, { noAuth: true });
}

export function enrollMfaConfirm(mfaToken: string, code: string) {
  return request<MfaEnrollConfirmResp>('POST', '/auth/mfa/enroll/confirm', { mfa_token: mfaToken, code }, { noAuth: true });
}

export function listPublicSsoProviders() {
  return request<PublicSsoProvidersResp>('GET', '/auth/sso/providers', undefined, { noAuth: true });
}

export function exchangeSsoCode(slug: string, code: string, state: string, redirectUri: string) {
  return request<LoginResponse>(
    'POST',
    `/auth/sso/${encodeURIComponent(slug)}/exchange`,
    { code, state, redirect_uri: redirectUri },
    { noAuth: true },
  );
}
