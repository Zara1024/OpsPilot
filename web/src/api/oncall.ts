import { getToken } from '@/store/auth';
import { request } from './client';

export type RotationTimeRestrictionType =
  | 'none'
  | 'time_of_day'
  | 'weekday'
  | 'workday_cn'
  | 'holiday_cn';

export interface OnCallRotation {
  id?: number;
  schedule_id?: number;
  name: string;
  tier: number; // 1=Primary, 2=Secondary
  rotation_type: 'daily' | 'weekly' | 'custom';
  shift_length_seconds: number;
  users?: number[];
  users_json?: string;
  effective_from: string;
  time_restriction_type: RotationTimeRestrictionType;
  restriction_start_time?: string;
  restriction_end_time?: string;
}

export function getRotationUserIds(rotation?: OnCallRotation | null): number[] {
  if (!rotation) return [];
  if (Array.isArray(rotation.users) && rotation.users.length > 0) {
    return rotation.users;
  }
  if (rotation.users_json) {
    try {
      const parsed = JSON.parse(rotation.users_json);
      if (Array.isArray(parsed)) return parsed.map((id) => Number(id));
    } catch {
      // ignore malformed json
    }
  }
  return [];
}

export interface SetSecondaryRotationInput {
  name?: string;
  rotation_type: 'daily' | 'weekly' | 'custom';
  shift_length_seconds?: number;
  users: number[];
  effective_from?: string;
  time_restriction_type?: RotationTimeRestrictionType;
  restriction_start_time?: string;
  restriction_end_time?: string;
}

export interface OnCallSchedule {
  id: number;
  team_id: number;
  name: string;
  description: string;
  timezone: string;
  handoff_time: string;
  reminder_advance_hours: number;
  require_swap_approval: boolean;
  escalation_repeat_count?: number;
  escalation_repeat_interval_seconds?: number;
  enabled: boolean;
  created_by: number;
  created_at: string;
  updated_at: string;
  rotations?: OnCallRotation[];
}

export interface ShiftSlot {
  schedule_id: number;
  schedule_name: string;
  rotation_id: number;
  rotation_name: string;
  tier: number; // 1=Primary, 2=Secondary
  user_id: number;
  user_name: string;
  user_email: string;
  user_phone: string;
  start_time: string;
  end_time: string;
  is_override: boolean;
  original_user_id?: number;
  original_user_name?: string;
  reason?: string;
  day_tag?: 'holiday' | 'makeup_workday' | string;
  day_label?: string;
}

export interface GapSlot {
  schedule_id: number;
  tier: number;
  start_time: string;
  end_time: string;
  duration_seconds: number;
  description: string;
}

export interface LiveOnCallStatus {
  schedule_id: number;
  schedule_name: string;
  primary_user?: ShiftSlot;
  secondary_user?: ShiftSlot;
  next_shift?: ShiftSlot;
  remaining_seconds: number;
  handoff_time: string;
}

export interface CalendarTokenResponse {
  token: string;
  webcal_url: string;
  http_url: string;
}

export interface CalendarResponse {
  shifts: ShiftSlot[];
  gaps: GapSlot[];
  start: string;
  end: string;
}

export async function listSchedules(): Promise<{ items: OnCallSchedule[]; total: number }> {
  return request('GET', '/oncall/schedules');
}

export async function getSchedule(id: number): Promise<OnCallSchedule> {
  return request('GET', `/oncall/schedules/${id}`);
}

export async function createSchedule(data: {
  name: string;
  description?: string;
  timezone?: string;
  handoff_time?: string;
  reminder_advance_hours?: number;
  require_swap_approval?: boolean;
  escalation_repeat_count?: number;
  escalation_repeat_interval_seconds?: number;
  enabled?: boolean;
  rotations?: OnCallRotation[];
}): Promise<OnCallSchedule> {
  return request('POST', '/oncall/schedules', data);
}

export async function updateSchedule(
  id: number,
  data: Partial<OnCallSchedule> & { rotations?: OnCallRotation[] }
): Promise<OnCallSchedule> {
  return request('PUT', `/oncall/schedules/${id}`, data);
}

export async function deleteSchedule(id: number): Promise<{ ok: boolean }> {
  return request('DELETE', `/oncall/schedules/${id}`);
}

export async function getCalendarShifts(
  id: number,
  start?: string,
  end?: string
): Promise<CalendarResponse> {
  const params = new URLSearchParams();
  if (start) params.set('start', start);
  if (end) params.set('end', end);
  const q = params.toString();
  return request('GET', `/oncall/schedules/${id}/calendar${q ? `?${q}` : ''}`);
}

export async function getCurrentOnCall(id: number): Promise<LiveOnCallStatus> {
  return request('GET', `/oncall/schedules/${id}/current`);
}

export async function getScheduleGaps(
  id: number,
  start?: string,
  end?: string
): Promise<{ gaps: GapSlot[]; total: number }> {
  const params = new URLSearchParams();
  if (start) params.set('start', start);
  if (end) params.set('end', end);
  const q = params.toString();
  return request('GET', `/oncall/schedules/${id}/gaps${q ? `?${q}` : ''}`);
}

export async function createOverride(
  scheduleId: number,
  data: {
    rotation_id?: number;
    type?: 'override' | 'swap';
    original_user_id?: number;
    substitute_user_id: number;
    start_time: string;
    end_time: string;
    reason?: string;
    status?: 'approved' | 'pending';
  }
): Promise<unknown> {
  return request('POST', `/oncall/schedules/${scheduleId}/overrides`, data);
}

export async function getMyCalendarToken(): Promise<CalendarTokenResponse> {
  return request('GET', '/oncall/users/me/calendar/token');
}

export async function resetMyCalendarToken(): Promise<CalendarTokenResponse> {
  return request('POST', '/oncall/users/me/calendar/token/reset');
}

// Phase 2: Routes, Approvals & Secondary Rotation

export interface LabelMatcher {
  name: string;
  operator: '=' | '!=' | '=~' | '!~';
  value: string;
}

export interface OnCallRoute {
  id: number;
  team_id: number;
  name: string;
  matcher_json: string;
  matchers?: LabelMatcher[];
  group_by_json?: string;
  group_by?: string[];
  priority: number;
  schedule_id: number;
  escalation_policy_id?: number;
  group_wait_seconds: number;
  group_interval_seconds: number;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface RouteMatchResult {
  matched_route?: OnCallRoute;
  schedule_id: number;
  schedule_name: string;
  is_default: boolean;
  matched_rules?: string[];
}

export interface PendingOverride {
  id: number;
  schedule_id: number;
  rotation_id: number;
  type: string;
  original_user_id: number;
  substitute_user_id: number;
  start_time: string;
  end_time: string;
  reason: string;
  status: 'pending' | 'approved' | 'rejected';
  schedule_name: string;
  original_user_name?: string;
  substitute_user_name?: string;
  created_at: string;
}

export async function listRoutes(): Promise<{ items: OnCallRoute[]; total: number }> {
  return request('GET', '/oncall/routes');
}

export async function createRoute(
  data: Partial<OnCallRoute> & { matchers?: LabelMatcher[] }
): Promise<OnCallRoute> {
  return request('POST', '/oncall/routes', data);
}

export async function updateRoute(
  id: number,
  data: Partial<OnCallRoute> & { matchers?: LabelMatcher[] }
): Promise<OnCallRoute> {
  return request('PUT', `/oncall/routes/${id}`, data);
}

export async function deleteRoute(id: number): Promise<{ ok: boolean }> {
  return request('DELETE', `/oncall/routes/${id}`);
}

export async function testMatchRoute(
  labels: Record<string, string>
): Promise<RouteMatchResult> {
  return request('POST', '/oncall/routes/test-match', { labels });
}

export async function listPendingOverrides(): Promise<{
  items: PendingOverride[];
  total: number;
}> {
  return request('GET', '/oncall/pending-overrides');
}

export async function approveOverride(
  id: number
): Promise<{ ok: boolean; status: string }> {
  return request('POST', `/oncall/overrides/${id}/approve`);
}

export async function rejectOverride(
  id: number
): Promise<{ ok: boolean; status: string }> {
  return request('POST', `/oncall/overrides/${id}/reject`);
}

export async function setSecondaryRotation(
  scheduleId: number,
  data: SetSecondaryRotationInput
): Promise<{ ok: boolean; rotation?: OnCallRotation }> {
  return request('POST', `/oncall/schedules/${scheduleId}/secondary-rotation`, data);
}


// ==========================================
// Phase 3: Personal Notification Rules, Escalation Policies & ChatOps
// ==========================================

export interface UserNotificationRule {
  id?: number;
  user_id?: number;
  urgency: 'high' | 'low';
  step_number: number;
  delay_minutes: number;
  channel: 'im_dm' | 'im_group_at' | 'sms' | 'voice_call' | 'email';
  enabled: boolean;
}

export interface EscalationPolicy {
  id?: number;
  schedule_id: number;
  name: string;
  step_number: number;
  wait_seconds: number;
  target_type: 'primary_oncall' | 'secondary_oncall' | 'specific_user' | 'webhook' | 'fallback_lead';
  target_id?: number;
}

export interface ActiveIncident {
  id: string;
  title: string;
  severity: 'P0' | 'P1' | 'P2' | 'P3';
  schedule_id: number;
  schedule_name: string;
  current_assignee: string;
  current_tier: number;
  status: 'firing' | 'acknowledged' | 'resolved' | 'silenced';
  started_at: string;
  acked_at?: string;
  acked_by?: string;
  escalation_step: number;
  war_room_url?: string;
}

export interface ChatOpsActionResult {
  ok: boolean;
  action: string;
  status_text: string;
  message: string;
  escalation_halt: boolean;
  war_room_url?: string;
}

export async function getMyNotificationRules(
  urgency?: string
): Promise<{ items: UserNotificationRule[]; total: number }> {
  const q = urgency ? `?urgency=${urgency}` : '';
  return request('GET', `/oncall/users/me/notification-rules${q}`);
}

export async function setMyNotificationRules(
  urgency: string,
  rules: UserNotificationRule[]
): Promise<{ ok: boolean; items: UserNotificationRule[] }> {
  return request('PUT', '/oncall/users/me/notification-rules', { urgency, rules });
}

export async function testNotification(
  channel: string,
  urgency: string
): Promise<{
  ok: boolean;
  channel: string;
  target: string;
  status?: string;
  message: string;
  result?: unknown;
}> {
  return request('POST', '/oncall/users/me/notification-rules/test', { channel, urgency });
}

export async function listEscalationPolicies(
  scheduleId?: number
): Promise<{ items: EscalationPolicy[]; total: number }> {
  const q = scheduleId ? `?schedule_id=${scheduleId}` : '';
  return request('GET', `/oncall/escalation-policies${q}`);
}

export async function createEscalationPolicy(
  data: Omit<EscalationPolicy, 'id'>
): Promise<EscalationPolicy> {
  return request('POST', '/oncall/escalation-policies', data);
}

export async function updateEscalationPolicy(
  id: number,
  data: Partial<EscalationPolicy>
): Promise<EscalationPolicy> {
  return request('PUT', `/oncall/escalation-policies/${id}`, data);
}

export async function deleteEscalationPolicy(id: number): Promise<{ ok: boolean }> {
  return request('DELETE', `/oncall/escalation-policies/${id}`);
}

export async function triggerChatOpsAction(
  action: 'ack' | 'silence' | 'escalate' | 'reassign' | 'warroom',
  incidentId: string,
  note?: string,
  targetUser?: number
): Promise<ChatOpsActionResult> {
  return request('POST', '/oncall/chatops/callback', {
    action,
    incident_id: incidentId,
    note,
    target_user: targetUser,
  });
}

export async function listActiveIncidents(status?: string): Promise<{ items: ActiveIncident[]; total: number }> {
  const q = status ? `?status=${status}` : '';
  return request('GET', `/oncall/incidents/active${q}`);
}

// ==========================================
// Phase 4: Shift Handoff & SRE Burnout Analytics
// ==========================================

export interface HandoffCheckItem {
  id: string;
  title: string;
  category: 'incident' | 'change' | 'silence' | 'maintenance';
  completed: boolean;
}

export interface CurrentHandoffStatus {
  schedule_id: number;
  schedule_name: string;
  outgoing_user_id: number;
  outgoing_user_name: string;
  incoming_user_id: number;
  incoming_user_name: string;
  shift_start: string;
  shift_end: string;
  next_handoff_time: string;
  time_remaining: string;
  status: 'pending' | 'signed_off';
  notes: string;
  sign_off_at?: string;
  active_incidents_summary: string[];
  pending_checklist: HandoffCheckItem[];
}

export interface HandoffLogItem {
  id: number;
  schedule_id: number;
  schedule_name?: string;
  user_id: number;
  user_name?: string;
  previous_user_id?: number;
  previous_user_name?: string;
  shift_start: string;
  shift_end: string;
  reminder_type: string;
  notes: string;
  ack_status: string;
  ack_at?: string;
  channel_type?: string;
  status?: string;
  error_message?: string;
  created_at: string;
}

export interface EngineerLoad {
  user_id: number;
  name: string;
  email: string;
  on_call_hours: number;
  incidents_handled: number;
  resolved_count?: number;
  avg_mtta_seconds?: number;
  night_calls: number;
  fatigue_score: number; // 0-100
  health_status: 'healthy' | 'moderate' | 'overloaded';
}

export interface TrendPoint {
  date: string;
  incident_count: number;
  night_count: number;
}

export interface SeveritySummary {
  severity: string;
  count: number;
  mtta_seconds: number;
  mttr_seconds: number;
}

export interface BurnoutAnalytics {
  time_range?: string;
  window_start?: string;
  window_end?: string;
  mtta_seconds: number;
  mtta_p50_seconds?: number;
  mtta_p90_seconds?: number;
  mtta_p95_seconds?: number;
  mtta_rating: 'good' | 'moderate' | 'poor' | 'warning' | 'critical';
  mttr_seconds?: number;
  mttr_p50_seconds?: number;
  mttr_p90_seconds?: number;
  mttr_p95_seconds?: number;
  mttr_rating?: 'good' | 'moderate' | 'poor' | 'warning' | 'critical';
  night_call_count: number;
  total_incidents: number;
  acknowledged_count?: number;
  resolved_count?: number;
  open_count?: number;
  ack_rate?: number;
  resolve_rate?: number;
  burnout_index: number; // 0-100
  merged_alerts_count?: number;
  suppressed_alerts_count?: number;
  noise_reduction_ratio: number;
  severity_breakdown?: SeveritySummary[];
  engineer_loads: EngineerLoad[];
  trends: TrendPoint[];
}

export interface NoisyAlertItem {
  fingerprint: string;
  alert_name: string;
  service: string;
  severity: string;
  trigger_count: number;
  avg_duration_sec: number;
  flapping_score: number;
  noise_level: 'extreme' | 'high' | 'medium';
  recommended_action: string;
}

export async function getCurrentHandoff(scheduleId?: number): Promise<CurrentHandoffStatus> {
  const q = scheduleId ? `?schedule_id=${scheduleId}` : '';
  return request('GET', `/oncall/handoff/current${q}`);
}

export async function listHandoffLogs(
  scheduleId?: number,
  limit?: number
): Promise<{ items: HandoffLogItem[]; total: number }> {
  const p = new URLSearchParams();
  if (scheduleId) p.set('schedule_id', String(scheduleId));
  if (limit) p.set('limit', String(limit));
  const q = p.toString() ? `?${p.toString()}` : '';
  return request('GET', `/oncall/handoff/logs${q}`);
}

export async function ackHandoff(data: {
  schedule_id: number;
  notes: string;
}): Promise<CurrentHandoffStatus> {
  return request('POST', '/oncall/handoff/ack', data);
}

export async function sendHandoffReminderNow(scheduleId: number): Promise<{
  ok: boolean;
  status: string;
  channel_type: string;
  error_message?: string;
  log?: HandoffLogItem;
}> {
  return request('POST', '/oncall/handoff/remind-now', { schedule_id: scheduleId });
}

export async function getBurnoutAnalytics(
  scheduleId?: number,
  timeRange?: string
): Promise<BurnoutAnalytics> {
  const p = new URLSearchParams();
  if (scheduleId) p.set('schedule_id', String(scheduleId));
  if (timeRange) p.set('time_range', timeRange);
  const q = p.toString() ? `?${p.toString()}` : '';
  return request('GET', `/oncall/analytics/burnout${q}`);
}

export async function getNoisyAlerts(
  scheduleId?: number,
  timeRange?: string
): Promise<{ items: NoisyAlertItem[]; total: number }> {
  const p = new URLSearchParams();
  if (scheduleId) p.set('schedule_id', String(scheduleId));
  if (timeRange) p.set('time_range', timeRange);
  const q = p.toString() ? `?${p.toString()}` : '';
  return request('GET', `/oncall/analytics/noisy-alerts${q}`);
}

// ==========================================
// Voice Gateway: Aliyun / Tencent / Mock
// ==========================================

export interface VoiceGatewayConfig {
  provider: 'mock' | 'aliyun' | 'tencent';
  fallback_provider?: '' | 'aliyun' | 'tencent';
  failover_timeout_seconds?: number;
  dispatch_enabled?: boolean;
  voice_redial_max_attempts?: number;
  voice_redial_delay_seconds?: number;
  voice_fast_escalate_on_reject?: boolean;
  voice_redial_on_no_dtmf?: boolean;
  aliyun_access_key_id?: string;
  aliyun_access_key_secret?: string;
  aliyun_secret_configured?: boolean;
  aliyun_called_show_number?: string;
  aliyun_tts_code?: string;
  aliyun_region?: string;
  tencent_secret_id?: string;
  tencent_secret_key?: string;
  tencent_secret_configured?: boolean;
  tencent_sdk_app_id?: string;
  tencent_template_id?: string;
  tencent_called_show_number?: string;
  tencent_region?: string;
  callback_urls?: Record<string, string>;
  updated_by?: number;
  updated_at?: string;
}

export interface VoiceCallRequest {
  call_id?: string;
  phone_number?: string;
  urgency?: 'high' | 'low';
  title?: string;
  severity?: string;
  force_mock?: boolean;
}

export interface VoiceCallResult {
  call_id: string;
  provider: string;
  fallback_from?: string;
  primary_error?: string;
  status: 'calling' | 'answered' | 'dtmf_confirmed' | 'throttled' | 'rejected' | 'no_answer' | 'unreachable' | 'failed';
  out_call_id?: string;
  phone_number: string;
  tts_content: string;
  dtmf_code?: string;
  duration_secs: number;
  message: string;
  raw_response?: string;
}

export interface OnCallNotificationLog {
  id: number;
  task_id: number;
  incident_id: number;
  alert_count?: number;
  schedule_id: number;
  escalation_step: number;
  user_id: number;
  user_name: string;
  target: string;
  channel: string;
  provider: string;
  call_id?: string;
  out_call_id?: string;
  status: string;
  dtmf_code?: string;
  duration_secs: number;
  message: string;
  is_night: boolean;
  created_at: string;
}

export interface OnCallEscalationTask {
  id: number;
  incident_id: number;
  group_key?: string;
  merged_incident_ids_json?: string;
  alert_count?: number;
  rule_key: string;
  title: string;
  severity: string;
  urgency: string;
  schedule_id: number;
  route_id: number;
  current_step: number;
  ladder_step: number;
  assignee_user_id: number;
  status: 'pending' | 'acked' | 'cancelled' | 'completed' | 'failed';
  next_run_at: string;
  attempt_count: number;
  last_error?: string;
  created_at: string;
  updated_at: string;
}

export async function getVoiceConfig(): Promise<VoiceGatewayConfig> {
  return request('GET', '/oncall/voice/config');
}

export async function updateVoiceConfig(
  data: VoiceGatewayConfig
): Promise<{ ok: boolean; config: VoiceGatewayConfig }> {
  return request('PUT', '/oncall/voice/config', data);
}

export async function testVoiceCall(
  data: VoiceCallRequest
): Promise<VoiceCallResult> {
  return request('POST', '/oncall/voice/test-call', data);
}

export async function listNotificationLogs(params?: {
  page?: number;
  page_size?: number;
  channel?: string;
  status?: string;
}): Promise<{ items: OnCallNotificationLog[]; total: number; page: number; page_size: number }> {
  const sp = new URLSearchParams();
  if (params?.page != null) sp.set('page', String(params.page));
  if (params?.page_size != null) sp.set('page_size', String(params.page_size));
  if (params?.channel && params.channel !== 'all') sp.set('channel', params.channel);
  if (params?.status && params.status !== 'all') sp.set('status', params.status);
  const q = sp.toString();
  return request('GET', `/oncall/notifications/logs${q ? `?${q}` : ''}`);
}

export interface ChannelGatewayConfig {
  sms_enabled: boolean;
  sms_provider: '' | 'aliyun' | 'tencent';
  aliyun_access_key_id?: string;
  aliyun_access_key_secret?: string;
  aliyun_secret_configured?: boolean;
  aliyun_sign_name?: string;
  aliyun_template_code?: string;
  aliyun_region?: string;
  tencent_secret_id?: string;
  tencent_secret_key?: string;
  tencent_secret_configured?: boolean;
  tencent_sdk_app_id?: string;
  tencent_sign_name?: string;
  tencent_template_id?: string;
  tencent_region?: string;

  email_enabled: boolean;
  smtp_host?: string;
  smtp_port: number;
  smtp_username?: string;
  smtp_password?: string;
  smtp_password_configured?: boolean;
  smtp_from?: string;
  smtp_tls_mode: 'ssl' | 'starttls' | 'none';
  smtp_timeout_seconds: number;

  chatops_token_configured?: boolean;
  feishu_verification_token?: string;
  feishu_verification_configured?: boolean;
  feishu_encrypt_key?: string;
  feishu_encrypt_configured?: boolean;
  dingtalk_app_secret?: string;
  dingtalk_secret_configured?: boolean;
  wecom_corp_id?: string;
  wecom_token?: string;
  wecom_token_configured?: boolean;
  wecom_encoding_aes_key?: string;
  wecom_aes_key_configured?: boolean;

  war_room_provider: '' | 'feishu' | 'webhook';
  feishu_app_id?: string;
  feishu_app_secret?: string;
  feishu_app_secret_configured?: boolean;
  war_room_webhook_url?: string;
  war_room_webhook_secret?: string;
  war_room_webhook_secret_configured?: boolean;

  callback_urls?: Record<string, string>;
  updated_by?: number;
  updated_at?: string;
}

export interface TestSMSResult {
  provider: string;
  status: 'sent' | 'failed';
  request_id?: string;
  biz_id?: string;
  phone_number: string;
  message: string;
}

export interface TestEmailResult {
  provider: string;
  status: 'sent' | 'failed';
  to: string;
  subject: string;
  message: string;
}

export async function getChannelConfig(): Promise<ChannelGatewayConfig> {
  return request('GET', '/oncall/channels/config');
}

export async function updateChannelConfig(
  data: ChannelGatewayConfig
): Promise<{ ok: boolean; config: ChannelGatewayConfig }> {
  return request('PUT', '/oncall/channels/config', data);
}

export async function testSMSChannel(data: {
  phone_number?: string;
  title?: string;
  urgency?: 'high' | 'low';
}): Promise<TestSMSResult> {
  return request('POST', '/oncall/channels/test-sms', data);
}

export async function testEmailChannel(data: {
  to?: string;
  subject?: string;
  urgency?: 'high' | 'low';
}): Promise<TestEmailResult> {
  return request('POST', '/oncall/channels/test-email', data);
}

export async function listEscalationTasks(
  status?: string
): Promise<{ items: OnCallEscalationTask[]; total: number }> {
  const q = status && status !== 'all' ? `?status=${encodeURIComponent(status)}` : '';
  return request('GET', `/oncall/escalations/tasks${q}`);
}

// ==========================================
// Phase 5 (P2): Statutory Holidays, Maintenance Windows, Export & Token Rotation
// ==========================================

export interface CalendarDayItem {
  id?: number;
  date: string;
  region: string;
  day_type: 'holiday' | 'workday';
  name: string;
  source: 'builtin' | 'custom';
  created_by?: number;
  created_at?: string;
  updated_at?: string;
}

export async function listCalendarDays(
  year?: number,
  region = 'CN'
): Promise<{ year: number; region: string; years: number[]; items: CalendarDayItem[]; total: number }> {
  const p = new URLSearchParams();
  if (year) p.set('year', String(year));
  if (region) p.set('region', region);
  return request('GET', `/oncall/calendar/days?${p.toString()}`);
}

export async function upsertCalendarDay(data: {
  date: string;
  day_type: 'holiday' | 'workday';
  name: string;
  region?: string;
}): Promise<{ ok: boolean; item: CalendarDayItem }> {
  return request('PUT', '/oncall/calendar/days', data);
}

export async function syncCNCalendarDays(year: number): Promise<{
  ok: boolean;
  year: number;
  synced_count: number;
}> {
  return request('POST', '/oncall/calendar/days/sync-cn', { year });
}

export async function deleteCalendarDay(id: number): Promise<{ ok: boolean }> {
  return request('DELETE', `/oncall/calendar/days/${id}`);
}

export interface MaintenanceWindowItem {
  id: number;
  name: string;
  description: string;
  schedule_id: number;
  schedule_name: string;
  matcher_json: string;
  matchers?: LabelMatcher[];
  action: 'suppress_all' | 'suppress_voice';
  starts_at: string;
  ends_at: string;
  status: 'active' | 'cancelled';
  runtime_status: 'running' | 'scheduled' | 'expired' | 'cancelled' | 'unknown';
  suppressed_count: number;
  created_by: number;
  created_at: string;
  updated_at: string;
}

export async function listMaintenanceWindows(
  status?: string
): Promise<{ items: MaintenanceWindowItem[]; total: number }> {
  const q = status && status !== 'all' ? `?status=${encodeURIComponent(status)}` : '';
  return request('GET', `/oncall/maintenance-windows${q}`);
}

export async function createMaintenanceWindow(data: {
  name: string;
  description?: string;
  schedule_id: number;
  matchers?: LabelMatcher[];
  action: 'suppress_all' | 'suppress_voice';
  starts_at: string;
  ends_at: string;
}): Promise<MaintenanceWindowItem> {
  return request('POST', '/oncall/maintenance-windows', data);
}

export async function cancelMaintenanceWindow(id: number): Promise<{
  ok: boolean;
  item: MaintenanceWindowItem;
}> {
  return request('POST', `/oncall/maintenance-windows/${id}/cancel`, {});
}

export async function deleteMaintenanceWindow(id: number): Promise<{ ok: boolean }> {
  return request('DELETE', `/oncall/maintenance-windows/${id}`);
}

export async function exportBurnoutAnalytics(params: {
  scheduleId?: number;
  timeRange?: string;
  format: 'csv' | 'json';
}): Promise<void> {
  const p = new URLSearchParams();
  if (params.scheduleId) p.set('schedule_id', String(params.scheduleId));
  if (params.timeRange) p.set('time_range', params.timeRange);
  p.set('format', params.format);

  const token = getToken();
  const res = await fetch(`/api/v1/oncall/analytics/export?${p.toString()}`, {
    method: 'GET',
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  });
  if (!res.ok) {
    throw new Error(`HTTP ${res.status}`);
  }
  const blob = await res.blob();
  const dlUrl = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = dlUrl;
  a.download = `opspilot-oncall-report-${params.timeRange || '30d'}.${params.format}`;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(dlUrl);
}

export async function rotateVoiceCallbackToken(): Promise<{
  ok: boolean;
  config: VoiceGatewayConfig;
}> {
  return request('POST', '/oncall/voice/callback-token/rotate', {});
}

export async function rotateChannelCallbackToken(): Promise<{
  ok: boolean;
  config: ChannelGatewayConfig;
}> {
  return request('POST', '/oncall/channels/callback-token/rotate', {});
}
