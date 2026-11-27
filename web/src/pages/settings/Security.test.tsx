import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import AdminSecurityPage from './Security';
import { server } from '@/test/msw-server';
import { selectOption } from '@/test/select-option';

vi.mock('@/store/me', () => ({
  usePermissions: () => ({ isAdmin: true, canMutate: true, role: 'admin' }),
}));

describe('AdminSecurityPage', () => {
  beforeEach(() => {
    localStorage.setItem('opspilot-locale', 'zh-CN');
  });

  it('saves auth policy and creates a new enterprise SSO provider', async () => {
    let policySaved = false;
    const providers: unknown[] = [];
    server.use(
      http.get('/api/v1/iam/auth-policy', () =>
        HttpResponse.json({
          mfa_enforce_mode: 'off',
          allow_local_password_login: true,
          login_ip_limit: 20,
          login_ip_window_sec: 300,
          login_email_limit: 6,
          login_email_window_sec: 900,
        }),
      ),
      http.put('/api/v1/iam/auth-policy', async ({ request }) => {
        policySaved = true;
        return HttpResponse.json(await request.json());
      }),
      http.get('/api/v1/iam/sso/providers', () => HttpResponse.json({ items: providers, total: providers.length })),
      http.post('/api/v1/iam/sso/providers', async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>;
        const created = {
          id: 1,
          ...body,
          has_client_secret: true,
          created_at: '2026-11-27T00:00:00Z',
          updated_at: '2026-11-27T00:00:00Z',
        };
        providers.push(created);
        return HttpResponse.json(created, { status: 201 });
      }),
      http.get('/api/v1/orgs', () => HttpResponse.json({ items: [{ id: 1, name: '默认组织' }], total: 1 })),
    );

    render(
      <MemoryRouter>
        <AdminSecurityPage />
      </MemoryRouter>,
    );

    await screen.findByText('全局登录与 MFA 策略');
    await selectOption(screen.getByRole('combobox', { name: 'MFA 强制范围' }), '仅强制管理员开启 MFA');
    await userEvent.click(screen.getByRole('button', { name: '保存策略' }));
    await waitFor(() => expect(policySaved).toBe(true));

    await userEvent.click(screen.getByRole('button', { name: '添加 SSO 提供商' }));
    await userEvent.type(screen.getByLabelText('显示名称'), 'Okta Corp');
    await userEvent.type(screen.getByLabelText('唯一 Slug 标识'), 'okta-corp');
    await userEvent.type(screen.getByLabelText('OIDC Issuer URL（支持 .well-known 自动发现）'), 'https://corp.okta.com');
    await userEvent.type(screen.getByLabelText('Client ID'), 'cli_okta_1');
    await userEvent.click(screen.getByRole('button', { name: '创建提供商' }));

    expect(await screen.findByText('Okta Corp')).toBeInTheDocument();
  });
});
