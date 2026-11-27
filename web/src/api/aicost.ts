import { request } from './client';

export type QuotaScopeType = 'global' | 'org' | 'user' | 'model';
export type QuotaActionOnExceed = 'block' | 'warn';
export type UsageStatus = 'ok' | 'error' | 'blocked';

export type WindowTotals = {
  requests: number;
  blocked_count: number;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  cost_usd: number;
};

export type DailyBucket = {
  date: string;
  requests: number;
  blocked_count: number;
  total_tokens: number;
  cost_usd: number;
};

export type GroupStat = {
  key: string;
  secondary?: string;
  requests: number;
  blocked_count: number;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  cost_usd: number;
};

export type QuotaView = {
  id: number;
  name: string;
  scope_type: QuotaScopeType;
  scope_key: string;
  enabled: boolean;
  daily_token_limit: number;
  monthly_token_limit: number;
  daily_cost_limit_usd: number;
  monthly_cost_limit_usd: number;
  warn_threshold_pct: number;
  action_on_exceed: QuotaActionOnExceed;
  circuit_open: boolean;
  circuit_opened_at?: string;
  circuit_reason?: string;
  description?: string;
  updated_at: string;
  daily_tokens_used: number;
  daily_cost_used_usd: number;
  monthly_tokens_used: number;
  monthly_cost_used_usd: number;
  max_utilization_pct: number;
  health_state: 'ok' | 'warn' | 'circuit_open';
};

export type AICostSummary = {
  days: number;
  today: WindowTotals;
  month: WindowTotals;
  overview: {
    totals: WindowTotals;
    daily: DailyBucket[];
    by_model: GroupStat[];
    by_source: GroupStat[];
    by_org: GroupStat[];
    by_user: GroupStat[];
  };
  quotas: QuotaView[];
  open_circuit_count: number;
};

export type UsageLog = {
  id: number;
  user_id: number;
  org_id: number;
  provider: string;
  model: string;
  source: string;
  session_id?: string;
  ref_id?: string;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  cost_usd: number;
  latency_ms: number;
  status: UsageStatus;
  error_message?: string;
  created_at: string;
};

export type QuotaInput = {
  name: string;
  scope_type: QuotaScopeType;
  scope_key: string;
  enabled: boolean;
  daily_token_limit: number;
  monthly_token_limit: number;
  daily_cost_limit_usd: number;
  monthly_cost_limit_usd: number;
  warn_threshold_pct: number;
  action_on_exceed: QuotaActionOnExceed;
  description?: string;
};

export type PriceEntry = {
  id?: number;
  provider: string;
  model_pattern: string;
  input_usd_per_1m: number;
  output_usd_per_1m: number;
  currency: string;
  custom: boolean;
};

export type PricesCatalog = {
  custom: PriceEntry[];
  builtin: PriceEntry[];
};

export function fetchAICostSummary(days = 7): Promise<AICostSummary> {
  return request<AICostSummary>('GET', `/ai-cost/summary?days=${days}`);
}

export function fetchAICostLogs(params: {
  days?: number;
  source?: string;
  status?: string;
  model?: string;
  limit?: number;
  offset?: number;
}): Promise<{ items: UsageLog[]; total: number }> {
  const qs = new URLSearchParams();
  if (params.days) qs.set('days', String(params.days));
  if (params.source) qs.set('source', params.source);
  if (params.status) qs.set('status', params.status);
  if (params.model) qs.set('model', params.model);
  if (params.limit != null) qs.set('limit', String(params.limit));
  if (params.offset != null) qs.set('offset', String(params.offset));
  const suffix = qs.toString() ? `?${qs.toString()}` : '';
  return request<{ items: UsageLog[]; total: number }>('GET', `/ai-cost/logs${suffix}`);
}

export function createAICostQuota(body: QuotaInput): Promise<QuotaView> {
  return request<QuotaView>('POST', '/ai-cost/quotas', body);
}

export function updateAICostQuota(id: number, body: QuotaInput): Promise<QuotaView> {
  return request<QuotaView>('PUT', `/ai-cost/quotas/${id}`, body);
}

export function deleteAICostQuota(id: number): Promise<void> {
  return request<void>('DELETE', `/ai-cost/quotas/${id}`);
}

export function tripAICostCircuit(id: number, reason?: string): Promise<QuotaView> {
  return request<QuotaView>('POST', `/ai-cost/quotas/${id}/trip`, { reason: reason || '' });
}

export function resetAICostCircuit(id: number): Promise<QuotaView> {
  return request<QuotaView>('POST', `/ai-cost/quotas/${id}/reset`, {});
}

export function fetchAICostPrices(): Promise<PricesCatalog> {
  return request<PricesCatalog>('GET', '/ai-cost/prices');
}

export function upsertAICostPrice(body: {
  provider: string;
  model_pattern: string;
  input_usd_per_1m: number;
  output_usd_per_1m: number;
  currency?: string;
}): Promise<PriceEntry> {
  return request<PriceEntry>('PUT', '/ai-cost/prices', body);
}

export function deleteAICostPrice(id: number): Promise<void> {
  return request<void>('DELETE', `/ai-cost/prices/${id}`);
}
