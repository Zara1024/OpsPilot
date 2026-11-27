import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { beforeEach, describe, expect, it } from 'vitest';

import LoginPage from './Login';
import { server } from '@/test/msw-server';
import { useAuth } from '@/store/auth';

describe('LoginPage SSO and MFA flows', () => {
  beforeEach(() => {
    localStorage.setItem('opspilot-locale', 'zh-CN');
    useAuth.getState().logout();
  });

  it('renders enabled enterprise SSO providers and completes 2-step MFA login', async () => {
    server.use(
      http.get('/api/v1/auth/sso/providers', () =>
        HttpResponse.json({
          items: [{ id: 1, slug: 'feishu-corp', name: '飞书单点登录', type: 'feishu' }],
          allow_local_password_login: true,
        }),
      ),
      http.post('/api/v1/auth/login', () =>
        HttpResponse.json({
          mfa_required: true,
          mfa_token: 'mfa-chal-tok',
          role: 'admin',
          email: 'admin@test.com',
        }),
      ),
      http.post('/api/v1/auth/mfa/verify', async ({ request }) => {
        const body = (await request.json()) as { mfa_token: string; code: string };
        expect(body.mfa_token).toBe('mfa-chal-tok');
        expect(body.code).toBe('654321');
        return HttpResponse.json({
          access_token: 'acc-123',
          refresh_token: 'ref-123',
          expires_in: 900,
          role: 'admin',
          email: 'admin@test.com',
        });
      }),
    );

    render(
      <MemoryRouter>
        <LoginPage />
      </MemoryRouter>,
    );

    expect(await screen.findByText('飞书单点登录')).toBeInTheDocument();

    await userEvent.type(screen.getByLabelText('邮箱'), 'admin@test.com');
    await userEvent.type(screen.getByLabelText('密码'), 'SecretPass123!');
    await userEvent.click(screen.getByRole('button', { name: '登录' }));

    const mfaInput = await screen.findByLabelText('动态验证码 / 恢复码');
    await userEvent.type(mfaInput, '654321');
    await userEvent.click(screen.getByRole('button', { name: '验证并登录' }));

    await waitFor(() => expect(useAuth.getState().token).toBe('acc-123'));
    expect(useAuth.getState().role).toBe('admin');
  });

  it('handles mandatory MFA enrollment and one-time recovery codes before entering console', async () => {
    server.use(
      http.get('/api/v1/auth/sso/providers', () =>
        HttpResponse.json({ items: [], allow_local_password_login: true }),
      ),
      http.post('/api/v1/auth/login', () =>
        HttpResponse.json({
          mfa_setup_required: true,
          mfa_token: 'mfa-setup-tok',
          role: 'admin',
          email: 'root@test.com',
        }),
      ),
      http.post('/api/v1/auth/mfa/enroll/start', () =>
        HttpResponse.json({
          secret: 'JBSWY3DPEHPK3PXP',
          otpauth_uri: 'otpauth://totp/OpsPilot:root@test.com?secret=JBSWY3DPEHPK3PXP',
          issuer: 'OpsPilot',
        }),
      ),
      http.post('/api/v1/auth/mfa/enroll/confirm', () =>
        HttpResponse.json({
          access_token: 'acc-enrolled',
          refresh_token: 'ref-enrolled',
          role: 'admin',
          email: 'root@test.com',
          recovery_codes: ['AAAA-1111', 'BBBB-2222'],
        }),
      ),
    );

    render(
      <MemoryRouter>
        <LoginPage />
      </MemoryRouter>,
    );

    await userEvent.type(screen.getByLabelText('邮箱'), 'root@test.com');
    await userEvent.type(screen.getByLabelText('密码'), 'AdminPass123!');
    await userEvent.click(screen.getByRole('button', { name: '登录' }));

    expect(await screen.findByText('JBSWY3DPEHPK3PXP')).toBeInTheDocument();
    expect(screen.getByRole('img', { name: 'MFA 绑定二维码' })).toBeInTheDocument();

    await userEvent.type(screen.getByLabelText('输入 6 位验证码确认绑定'), '112233');
    await userEvent.click(screen.getByRole('button', { name: '完成绑定并进入控制台' }));

    expect(await screen.findByText('AAAA-1111')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '我已保存，进入 OpsPilot' }));
    await waitFor(() => expect(useAuth.getState().token).toBe('acc-enrolled'));
  });
});
