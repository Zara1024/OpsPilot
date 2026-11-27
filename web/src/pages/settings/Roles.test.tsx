import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import AdminRolesPage from './Roles';
import { server } from '@/test/msw-server';

vi.mock('@/store/me', () => ({
  usePermissions: () => ({ isAdmin: true, canMutate: true, role: 'admin' }),
}));

describe('AdminRolesPage', () => {
  beforeEach(() => {
    localStorage.setItem('opspilot-locale', 'zh-CN');
  });

  it('creates a resource RBAC grant and runs access simulator', async () => {
    const grants: unknown[] = [];
    server.use(
      http.get('/api/v1/iam/rbac/grants', () => HttpResponse.json({ items: grants, total: grants.length })),
      http.get('/api/v1/users', () =>
        HttpResponse.json({
          items: [
            { id: 1, email: 'admin@test.com', display_name: 'Admin', role: 'admin', status: 'active' },
            { id: 2, email: 'dev@test.com', display_name: 'Dev Alice', role: 'user', status: 'active' },
          ],
          total: 2,
        }),
      ),
      http.get('/api/v1/orgs', () => HttpResponse.json({ items: [{ id: 1, name: '默认组织' }], total: 1 })),
      http.post('/api/v1/iam/rbac/grants', async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>;
        const created = {
          id: 11,
          ...body,
          created_by: 1,
          created_at: '2026-11-27T00:00:00Z',
          updated_at: '2026-11-27T00:00:00Z',
        };
        grants.push(created);
        return HttpResponse.json(created, { status: 201 });
      }),
      http.post('/api/v1/iam/rbac/check', () =>
        HttpResponse.json({
          allowed: false,
          reason: 'explicit_deny',
          matched_grant_id: 11,
          matched_rule: 'user #2 deny device_env(exec) on prod*',
        }),
      ),
    );

    render(
      <MemoryRouter>
        <AdminRolesPage />
      </MemoryRouter>,
    );

    await screen.findByText('资源权限 (RBAC)');
    await userEvent.click(screen.getByRole('button', { name: '新建授权规则' }));
    await userEvent.click(screen.getByRole('button', { name: '创建规则' }));

    expect(await screen.findByText('prod*')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: '测试权限' }));
    await waitFor(() => expect(screen.getByText('拒绝访问 (DENY)')).toBeInTheDocument());
    expect(screen.getByText('explicit_deny')).toBeInTheDocument();
  });
});
