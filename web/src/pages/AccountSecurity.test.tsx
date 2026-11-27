import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import AccountSecurityPage from './AccountSecurity';
import { server } from '@/test/msw-server';

vi.mock('@/store/me', () => ({
  useMe: () => ({
    me: { id: 9, email: 'alice@test.com', role: 'user', status: 'active', display_name: 'Alice', phone: '', memberships: [] },
    loading: false,
    error: null,
    refresh: vi.fn().mockResolvedValue(undefined),
  }),
}));

describe('AccountSecurityPage', () => {
  beforeEach(() => {
    localStorage.setItem('opspilot-locale', 'zh-CN');
  });

  it('enrolls TOTP MFA and displays one-time recovery codes', async () => {
    let enabled = false;
    server.use(
      http.get('/api/v1/auth/mfa/status', () =>
        HttpResponse.json({
          enabled,
          recovery_codes_remaining: enabled ? 8 : 0,
          enforced: false,
        }),
      ),
      http.get('/api/v1/auth/identities', () => HttpResponse.json({ items: [], total: 0 })),
      http.post('/api/v1/auth/mfa/setup', () =>
        HttpResponse.json({
          secret: 'TESTBASE32SECRET',
          otpauth_uri: 'otpauth://totp/OpsPilot:alice@test.com?secret=TESTBASE32SECRET',
          issuer: 'OpsPilot',
        }),
      ),
      http.post('/api/v1/auth/mfa/confirm', () => {
        enabled = true;
        return HttpResponse.json({ recovery_codes: ['REC1-CODE', 'REC2-CODE'] });
      }),
    );

    render(
      <MemoryRouter>
        <AccountSecurityPage />
      </MemoryRouter>,
    );

    const enableBtn = await screen.findByRole('button', { name: '开启 TOTP MFA' });
    await userEvent.click(enableBtn);

    expect(await screen.findByText('TESTBASE32SECRET')).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText('2. 输入认证器生成的 6 位验证码确认绑定'), '123456');
    await userEvent.click(screen.getByRole('button', { name: '验证并启用' }));

    expect(await screen.findByText('REC1-CODE')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '我已妥善保存' }));
    await waitFor(() => expect(screen.getByText('已启用')).toBeInTheDocument());
  });
});
