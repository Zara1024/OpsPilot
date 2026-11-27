import { request } from './client';

export type MfaStatus = {
  enabled: boolean;
  recovery_codes_remaining: number;
  verified_at?: string;
  enforced?: boolean;
};

export type MfaSetupResult = {
  secret: string;
  otpauth_uri: string;
  issuer: string;
};

export type MfaRecoveryResp = {
  recovery_codes: string[];
};

export type LinkedSsoIdentity = {
  id: number;
  provider_id: number;
  provider_slug: string;
  provider_name: string;
  provider_type: string;
  external_sub: string;
  external_email: string;
  external_name: string;
  last_login_at?: string;
  created_at: string;
};

export type MfaEnforceMode = 'off' | 'admins' | 'all';

export type AuthPolicy = {
  mfa_enforce_mode: MfaEnforceMode;
  allow_local_password_login: boolean;
  login_ip_limit: number;
  login_ip_window_sec: number;
  login_email_limit: number;
  login_email_window_sec: number;
  updated_at?: string;
};

export type SsoProviderType = 'oidc' | 'feishu' | 'wecom';

export type SsoProvider = {
  id: number;
  slug: string;
  name: string;
  type: SsoProviderType;
  enabled: boolean;
  issuer_url?: string;
  auth_url?: string;
  token_url?: string;
  userinfo_url?: string;
  jwks_url?: string;
  client_id: string;
  has_client_secret: boolean;
  corp_id?: string;
  agent_id?: string;
  scopes?: string;
  auto_create_user: boolean;
  default_role: 'admin' | 'user' | 'viewer';
  default_org_id?: number | null;
  allowed_domains?: string;
  created_at: string;
  updated_at: string;
};

export type SsoProviderInput = {
  slug: string;
  name: string;
  type: SsoProviderType;
  enabled: boolean;
  issuer_url?: string;
  auth_url?: string;
  token_url?: string;
  userinfo_url?: string;
  jwks_url?: string;
  client_id: string;
  client_secret?: string;
  corp_id?: string;
  agent_id?: string;
  scopes?: string;
  auto_create_user: boolean;
  default_role: 'admin' | 'user' | 'viewer';
  default_org_id?: number | null;
  allowed_domains?: string;
};

// Personal MFA & linked identities

export function getMfaStatus() {
  return request<MfaStatus>('GET', '/auth/mfa/status');
}

export function setupMfa() {
  return request<MfaSetupResult>('POST', '/auth/mfa/setup');
}

export function confirmMfa(code: string) {
  return request<MfaRecoveryResp>('POST', '/auth/mfa/confirm', { code });
}

export function disableMfa(code: string) {
  return request<void>('POST', '/auth/mfa/disable', { code });
}

export function regenerateRecoveryCodes(code: string) {
  return request<MfaRecoveryResp>('POST', '/auth/mfa/recovery-codes/regenerate', { code });
}

export function listMyIdentities() {
  return request<{ items: LinkedSsoIdentity[]; total: number }>('GET', '/auth/identities');
}

export function unlinkMyIdentity(id: number) {
  return request<void>('DELETE', `/auth/identities/${id}`);
}

// Admin security governance

export function getAuthPolicy() {
  return request<AuthPolicy>('GET', '/iam/auth-policy');
}

export function updateAuthPolicy(body: AuthPolicy) {
  return request<AuthPolicy>('PUT', '/iam/auth-policy', body);
}

export function listSsoProviders() {
  return request<{ items: SsoProvider[]; total: number }>('GET', '/iam/sso/providers');
}

export function createSsoProvider(body: SsoProviderInput) {
  return request<SsoProvider>('POST', '/iam/sso/providers', body);
}

export function updateSsoProvider(id: number, body: SsoProviderInput) {
  return request<SsoProvider>('PUT', `/iam/sso/providers/${id}`, body);
}

export function deleteSsoProvider(id: number) {
  return request<void>('DELETE', `/iam/sso/providers/${id}`);
}

export function resetUserMfa(userId: number) {
  return request<void>('POST', `/users/${userId}/mfa/reset`);
}
