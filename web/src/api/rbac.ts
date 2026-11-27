import { request } from './client';

export type RbacSubjectType = 'user' | 'org';
export type RbacResourceType = 'device' | 'device_env' | 'k8s_cluster' | 'service';
export type RbacEffect = 'allow' | 'deny';

export type ResourceGrant = {
  id: number;
  subject_type: RbacSubjectType;
  subject_id: number;
  resource_type: RbacResourceType;
  resource_pattern: string;
  actions: string;
  effect: RbacEffect;
  description?: string;
  created_by: number;
  created_at: string;
  updated_at: string;
};

export type ResourceGrantInput = {
  subject_type: RbacSubjectType;
  subject_id: number;
  resource_type: RbacResourceType;
  resource_pattern: string;
  actions: string;
  effect: RbacEffect;
  description?: string;
};

export type RbacDecision = {
  allowed: boolean;
  reason: string;
  matched_grant_id?: number;
  matched_rule?: string;
};

export function listResourceGrants(params?: {
  subject_type?: string;
  subject_id?: number;
  resource_type?: string;
  effect?: string;
}) {
  const qs = new URLSearchParams();
  if (params?.subject_type) qs.set('subject_type', params.subject_type);
  if (params?.subject_id) qs.set('subject_id', String(params.subject_id));
  if (params?.resource_type) qs.set('resource_type', params.resource_type);
  if (params?.effect) qs.set('effect', params.effect);
  const suffix = qs.toString() ? `?${qs.toString()}` : '';
  return request<{ items: ResourceGrant[]; total: number }>('GET', `/iam/rbac/grants${suffix}`);
}

export function createResourceGrant(body: ResourceGrantInput) {
  return request<ResourceGrant>('POST', '/iam/rbac/grants', body);
}

export function updateResourceGrant(id: number, body: ResourceGrantInput) {
  return request<ResourceGrant>('PUT', `/iam/rbac/grants/${id}`, body);
}

export function deleteResourceGrant(id: number) {
  return request<void>('DELETE', `/iam/rbac/grants/${id}`);
}

export function checkResourceAccess(body: {
  user_id: number;
  resource_type: RbacResourceType;
  resource_keys: string[];
  action: string;
}) {
  return request<RbacDecision>('POST', '/iam/rbac/check', body);
}
