import { request } from './client';

export interface OnCallRotation {
  id?: number;
  schedule_id?: number;
  name: string;
  tier: number; // 1=Primary, 2=Secondary
  rotation_type: 'daily' | 'weekly' | 'custom';
  shift_length_seconds: number;
  users: number[];
  effective_from: string;
  time_restriction_type: 'none' | 'time_of_day' | 'weekday';
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
  data: {
    name?: string;
    rotation_type: 'daily' | 'weekly';
    shift_length_seconds?: number;
    users: number[];
    effective_from?: string;
    time_restriction_type?: 'none' | 'time_of_day' | 'weekday';
    restriction_start_time?: string;
    restriction_end_time?: string;
  }
): Promise<{ ok: boolean }> {
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
): Promise<{ ok: boolean; channel: string; target: string; message: string; result: any }> {
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
  note?: string
): Promise<ChatOpsActionResult> {
  return request('POST', '/oncall/chatops/callback', { action, incident_id: incidentId, note });
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
  user_id: number;
  previous_user_id?: number;
  shift_start: string;
  shift_end: string;
  reminder_type: string;
  notes: string;
  ack_status: string;
  ack_at?: string;
  created_at: string;
}

export interface EngineerLoad {
  user_id: number;
  name: string;
  email: string;
  on_call_hours: number;
  incidents_handled: number;
  night_calls: number;
  fatigue_score: number; // 0-100
  health_status: 'healthy' | 'moderate' | 'overloaded';
}

export interface TrendPoint {
  date: string;
  incident_count: number;
  night_count: number;
}

export interface BurnoutAnalytics {
  mtta_seconds: number;
  mtta_rating: 'good' | 'warning' | 'critical';
  night_call_count: number;
  total_incidents: number;
  burnout_index: number; // 0-100
  noise_reduction_ratio: number;
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
  scheduleId?: number
): Promise<{ items: NoisyAlertItem[]; total: number }> {
  const q = scheduleId ? `?schedule_id=${scheduleId}` : '';
  return request('GET', `/oncall/analytics/noisy-alerts${q}`);
}

// ==========================================
// Voice Gateway: Aliyun / Tencent / Mock
// ==========================================

export interface VoiceGatewayConfig {
  provider: 'mock' | 'aliyun' | 'tencent';
  aliyun_access_key_id?: string;
  aliyun_access_key_secret?: string;
  aliyun_called_show_number?: string;
  aliyun_tts_code?: string;
  aliyun_region?: string;
  tencent_secret_id?: string;
  tencent_secret_key?: string;
  tencent_sdk_app_id?: string;
  tencent_template_id?: string;
  tencent_called_show_number?: string;
  tencent_region?: string;
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
  status: 'calling' | 'answered' | 'dtmf_confirmed' | 'failed';
  out_call_id?: string;
  phone_number: string;
  tts_content: string;
  dtmf_code?: string;
  duration_secs: number;
  message: string;
  raw_response?: string;
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
