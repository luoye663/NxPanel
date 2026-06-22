import { del, get, post, put } from './client'

export interface AuthRule {
  id: string
  site_id: string
  name: string
  path: string
  username: string
  enabled: boolean
  sort_order: number
  created_at: string
  updated_at: string
}

export interface DenyRule {
  id: string
  site_id: string
  name: string
  extension_pattern: string
  path_pattern: string
  enabled: boolean
  sort_order: number
  created_at: string
  updated_at: string
}

export interface AuthRuleRequest {
  name?: string
  path?: string
  username?: string
  password?: string
  enabled?: boolean
}

export interface DenyRuleRequest {
  name?: string
  extension_pattern?: string
  path_pattern?: string
  enabled?: boolean
}

export interface IPLimitRule {
  id: string
  site_id: string
  name: string
  rule_type: 'allow' | 'deny'
  ips: string[]
  enabled: boolean
  sort_order: number
  created_at: string
  updated_at: string
}

export interface IPLimitRuleRequest {
  name?: string
  rule_type?: 'allow' | 'deny'
  ips_text?: string
  enabled?: boolean
}

export interface HotlinkRule {
  id: string
  site_id: string
  name: string
  enabled: boolean
  extensions: string[]
  referers: string[]
  allow_empty_referer: boolean
  block_status: 403 | 404 | 444
  sort_order: number
  created_at: string
  updated_at: string
}

export interface HotlinkRuleRequest {
  name?: string
  enabled?: boolean
  extensions?: string[]
  referers?: string[]
  allow_empty_referer?: boolean
  block_status?: 403 | 404 | 444
}

// 访问限制规则写入独立 include 文件，前端只维护结构化表单数据。
export function listAuthRules(siteId: string): Promise<AuthRule[]> {
  return get(`/sites/${siteId}/auth-rules`)
}

export function createAuthRule(siteId: string, data: Required<Pick<AuthRuleRequest, 'name' | 'path' | 'username' | 'password'>> & { enabled?: boolean }): Promise<AuthRule> {
  return post(`/sites/${siteId}/auth-rules`, data)
}

export function updateAuthRule(siteId: string, ruleId: string, data: AuthRuleRequest): Promise<AuthRule> {
  return put(`/sites/${siteId}/auth-rules/${ruleId}`, data)
}

export function deleteAuthRule(siteId: string, ruleId: string): Promise<{ deleted: boolean }> {
  return del(`/sites/${siteId}/auth-rules/${ruleId}`)
}

export function listDenyRules(siteId: string): Promise<DenyRule[]> {
  return get(`/sites/${siteId}/deny-rules`)
}

export function createDenyRule(siteId: string, data: Required<Pick<DenyRuleRequest, 'name' | 'extension_pattern' | 'path_pattern'>> & { enabled?: boolean }): Promise<DenyRule> {
  return post(`/sites/${siteId}/deny-rules`, data)
}

export function updateDenyRule(siteId: string, ruleId: string, data: DenyRuleRequest): Promise<DenyRule> {
  return put(`/sites/${siteId}/deny-rules/${ruleId}`, data)
}

export function deleteDenyRule(siteId: string, ruleId: string): Promise<{ deleted: boolean }> {
  return del(`/sites/${siteId}/deny-rules/${ruleId}`)
}

export function listIPLimitRules(siteId: string): Promise<IPLimitRule[]> {
  return get(`/sites/${siteId}/ip-limit-rules`)
}

export function createIPLimitRule(siteId: string, data: Required<Pick<IPLimitRuleRequest, 'name' | 'rule_type' | 'ips_text'>> & { enabled?: boolean }): Promise<IPLimitRule> {
  return post(`/sites/${siteId}/ip-limit-rules`, data)
}

export function updateIPLimitRule(siteId: string, ruleId: string, data: IPLimitRuleRequest): Promise<IPLimitRule> {
  return put(`/sites/${siteId}/ip-limit-rules/${ruleId}`, data)
}

export function deleteIPLimitRule(siteId: string, ruleId: string): Promise<{ deleted: boolean }> {
  return del(`/sites/${siteId}/ip-limit-rules/${ruleId}`)
}

export function listHotlinkRules(siteId: string): Promise<HotlinkRule[]> {
  return get(`/sites/${siteId}/hotlink-rules`)
}

export function createHotlinkRule(siteId: string, data: HotlinkRuleRequest): Promise<HotlinkRule> {
  return post(`/sites/${siteId}/hotlink-rules`, data)
}

export function updateHotlinkRule(siteId: string, ruleId: string, data: HotlinkRuleRequest): Promise<HotlinkRule> {
  return put(`/sites/${siteId}/hotlink-rules/${ruleId}`, data)
}

export function deleteHotlinkRule(siteId: string, ruleId: string): Promise<{ deleted: boolean }> {
  return del(`/sites/${siteId}/hotlink-rules/${ruleId}`)
}
