import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import AdminAICostPage from './AICost';
import { server } from '@/test/msw-server';

vi.mock('@/store/me', () => ({
  usePermissions: () => ({ isAdmin: true, canMutate: true, role: 'admin' }),
}));

describe('AdminAICostPage', () => {
  beforeEach(() => {
    localStorage.setItem('opspilot-locale', 'zh-CN');
  });

  it('renders cost KPIs, creates quota, trips & resets circuit breaker, and saves custom price', async () => {
    const quotas: Record<string, unknown>[] = [];
    const customPrices: Record<string, unknown>[] = [];

    server.use(
      http.get('/api/v1/ai-cost/summary', () =>
        HttpResponse.json({
          days: 7,
          today: { requests: 12, blocked_count: 1, prompt_tokens: 8000, completion_tokens: 2000, total_tokens: 10000, cost_usd: 0.42 },
          month: { requests: 120, blocked_count: 3, prompt_tokens: 90000, completion_tokens: 30000, total_tokens: 120000, cost_usd: 5.15 },
          overview: {
            totals: { requests: 40, blocked_count: 1, prompt_tokens: 30000, completion_tokens: 10000, total_tokens: 40000, cost_usd: 1.85 },
            daily: [{ date: '2026-11-27', requests: 12, blocked_count: 1, total_tokens: 10000, cost_usd: 0.42 }],
            by_model: [{ key: 'gpt-4o', secondary: 'openai', requests: 12, blocked_count: 0, prompt_tokens: 8000, completion_tokens: 2000, total_tokens: 10000, cost_usd: 0.42 }],
            by_source: [{ key: 'chat', requests: 10, blocked_count: 0, prompt_tokens: 6000, completion_tokens: 1500, total_tokens: 7500, cost_usd: 0.35 }],
            by_org: [],
            by_user: [],
          },
          quotas,
          open_circuit_count: quotas.filter((q) => Boolean(q.circuit_open)).length,
        }),
      ),
      http.get('/api/v1/ai-cost/prices', () =>
        HttpResponse.json({
          custom: customPrices,
          builtin: [{ provider: 'openai', model_pattern: 'gpt-4o*', input_usd_per_1m: 2.5, output_usd_per_1m: 10.0, currency: 'USD', custom: false }],
        }),
      ),
      http.get('/api/v1/orgs', () => HttpResponse.json({ items: [{ id: 1, name: '默认组织' }], total: 1 })),
      http.get('/api/v1/users', () => HttpResponse.json({ items: [{ id: 1, email: 'admin@test.com', role: 'admin', status: 'active' }], total: 1 })),
      http.post('/api/v1/ai-cost/quotas', async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>;
        const created = {
          id: 5,
          ...body,
          circuit_open: false,
          daily_tokens_used: 10000,
          daily_cost_used_usd: 0.42,
          monthly_tokens_used: 120000,
          monthly_cost_used_usd: 5.15,
          max_utilization_pct: 5,
          health_state: 'ok',
          updated_at: '2026-11-27T00:00:00Z',
        };
        quotas.push(created);
        return HttpResponse.json(created, { status: 201 });
      }),
      http.post('/api/v1/ai-cost/quotas/5/trip', () => {
        quotas[0] = { ...quotas[0], circuit_open: true, circuit_reason: 'manual: tripped by admin console', health_state: 'circuit_open' };
        return HttpResponse.json(quotas[0]);
      }),
      http.post('/api/v1/ai-cost/quotas/5/reset', () => {
        quotas[0] = { ...quotas[0], circuit_open: false, circuit_reason: '', health_state: 'ok' };
        return HttpResponse.json(quotas[0]);
      }),
      http.put('/api/v1/ai-cost/prices', async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>;
        const saved = { id: 9, ...body, currency: 'USD', custom: true };
        customPrices.push(saved);
        return HttpResponse.json(saved);
      }),
    );

    render(
      <MemoryRouter>
        <AdminAICostPage />
      </MemoryRouter>,
    );

    await screen.findByText('AI 成本与熔断');
    expect(await screen.findByText('$0.42')).toBeInTheDocument();

    // Create quota
    await userEvent.click(screen.getByRole('button', { name: '新建配额规则' }));
    await userEvent.type(screen.getByLabelText('规则名称'), '全局日预算守卫');
    await userEvent.click(screen.getByRole('button', { name: '创建配额' }));

    expect(await screen.findByText('全局日预算守卫')).toBeInTheDocument();

    // Manual trip & confirm dialog
    await userEvent.click(screen.getByRole('button', { name: '手动熔断' }));
    await userEvent.click(await screen.findByRole('button', { name: '确认' }));
    await waitFor(() => expect(screen.getByText('已熔断')).toBeInTheDocument());

    // Reset circuit
    await userEvent.click(screen.getByRole('button', { name: '恢复熔断' }));
    await userEvent.click(await screen.findByRole('button', { name: '确认' }));
    await waitFor(() => expect(screen.getByText('放行正常')).toBeInTheDocument());

    // Switch to pricing tab & add custom price
    await userEvent.click(screen.getByRole('tab', { name: '模型单价配置' }));
    await userEvent.click(screen.getByRole('button', { name: '设置自定义单价' }));
    await userEvent.type(screen.getByLabelText('模型匹配模式'), 'qwen-max*');
    await userEvent.click(screen.getByRole('button', { name: '保存单价' }));
    expect(await screen.findByText('qwen-max*')).toBeInTheDocument();
  });
});
