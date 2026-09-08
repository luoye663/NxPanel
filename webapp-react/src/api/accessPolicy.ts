import { get, post, put } from './client'

export interface AccessCondition {
  kind: 'ip' | 'country' | 'path' | 'extension' | 'referer'
  operator?: 'in' | 'exact' | 'prefix'
  values: string[]
  negate: boolean
}

export interface AccessAction {
  type: 'allow' | 'deny' | 'auth'
  status_code?: number
  response_type?: 'text' | 'html'
  response_body?: string
  account_ids?: string[]
}

export interface AccessPolicyRule {
  id: string
  name: string
  enabled: boolean
  match: 'all' | 'any'
  conditions: AccessCondition[]
  action: AccessAction
  source_type?: string
  source_id?: string
  source_disabled?: boolean
}

export interface AccessPolicy {
  version: number
  mode: 'legacy' | 'unified'
  rules: AccessPolicyRule[]
  default_action: AccessAction
  apply_status: string
  last_error?: string
  warnings?: string[]
  pending_source?: string
}

export interface AccessPreviewRequest {
  ip: string
  country?: string
  path: string
  referer?: string
}

export interface AccessPreviewResult {
  rules: { id: string; matched: boolean; conditions: boolean[]; evaluated: boolean }[]
  matched_rule_id: string
  action: AccessAction
  requires_authentication: boolean
}

export const accessPolicyKeys = {
  site: (siteId: string) => ['site-detail', siteId, 'access-policy'] as const,
}

function normalizePolicy(policy: AccessPolicy): AccessPolicy {
  return {
    ...policy,
    rules: (policy.rules || []).map((rule) => ({
      ...rule,
      conditions: (rule.conditions || []).map((condition) => ({ ...condition, values: condition.values || [] })),
    })),
  }
}

export function getAccessPolicy(siteId: string): Promise<AccessPolicy> {
  return get<AccessPolicy>(`/sites/${siteId}/access-policy`).then(normalizePolicy)
}

export function saveAccessPolicy(siteId: string, policy: AccessPolicy): Promise<AccessPolicy> {
  return put<AccessPolicy>(`/sites/${siteId}/access-policy`, policy).then(normalizePolicy)
}

export function syncAccessPolicy(siteId: string): Promise<AccessPolicy> {
  return post<AccessPolicy>(`/sites/${siteId}/access-policy/sync`).then(normalizePolicy)
}

export function previewAccessPolicy(siteId: string, policy: AccessPolicy, request: AccessPreviewRequest): Promise<AccessPreviewResult> {
  return post(`/sites/${siteId}/access-policy/preview`, { policy, request })
}
