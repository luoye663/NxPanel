import { del, get, http, post, put } from './client'

export type GeoAction = 'allow' | 'deny_403' | 'deny_444'
export type GeoDefaultAction = 'allow' | 'respond'
export type GeoResponseType = 'html' | 'text'

export interface GeoIPSettings {
  account_id: string
  license_key_masked: string
  auto_update: boolean
  trusted_proxies: string[]
}

export interface GeoIPStatus {
  installed: boolean
  checksum: string
  build_epoch: number
  countries: string[]
  last_attempt_at?: string
  last_success_at?: string
  last_error?: string
  enabled_sites: number
}

export interface SiteGeoAccess {
  site_id: string
  enabled: boolean
  default_action: GeoDefaultAction
  default_status_code: number
  default_response_type: GeoResponseType
  default_response_body: string
  desired_hash: string
  applied_hash: string
  apply_status: 'disabled' | 'pending' | 'applied' | 'error'
  last_error?: string
}

export interface GeoRule {
  id: string
  site_id: string
  name: string
  countries: string[]
  action: GeoAction
  enabled: boolean
  sort_order: number
  created_at: string
  updated_at: string
}

export interface GeoRuleRequest {
  name?: string
  countries?: string[]
  action?: GeoAction
  enabled?: boolean
}

export const geoAccessKeys = {
  settings: ['geo-access', 'settings'] as const,
  status: ['geo-access', 'status'] as const,
  site: (siteId: string) => ['geo-access', 'site', siteId] as const,
  rules: (siteId: string) => ['geo-access', 'rules', siteId] as const,
}

export function getGeoIPSettings(): Promise<GeoIPSettings> { return get('/geoip/settings') }
export function updateGeoIPSettings(data: { account_id: string; license_key?: string; auto_update: boolean; trusted_proxies: string[] }): Promise<GeoIPSettings> { return put('/geoip/settings', data) }
export function getGeoIPStatus(): Promise<GeoIPStatus> { return get('/geoip/status') }
export function updateGeoIPDatabase(): Promise<GeoIPStatus> { return post('/geoip/update') }
export function uploadGeoIPDatabase(file: File): Promise<GeoIPStatus> {
  const data = new FormData()
  data.append('database', file)
  return http.post<GeoIPStatus>('/geoip/database', data, { timeout: 120_000 }).then((response) => response.data)
}

export function getSiteGeoAccess(siteId: string): Promise<SiteGeoAccess> { return get(`/sites/${siteId}/geo-access`) }
export function updateSiteGeoAccess(siteId: string, data: Pick<SiteGeoAccess, 'default_action' | 'default_status_code' | 'default_response_type' | 'default_response_body'>): Promise<SiteGeoAccess> { return put(`/sites/${siteId}/geo-access`, data) }
export function enableSiteGeoAccess(siteId: string): Promise<SiteGeoAccess> { return post(`/sites/${siteId}/geo-access/enable`) }
export function disableSiteGeoAccess(siteId: string): Promise<SiteGeoAccess> { return post(`/sites/${siteId}/geo-access/disable`) }
export function listGeoRules(siteId: string): Promise<GeoRule[]> { return get(`/sites/${siteId}/geo-rules`) }
export function createGeoRule(siteId: string, data: Required<Pick<GeoRuleRequest, 'name' | 'countries' | 'action'>> & { enabled?: boolean }): Promise<GeoRule> { return post(`/sites/${siteId}/geo-rules`, data) }
export function updateGeoRule(siteId: string, ruleId: string, data: GeoRuleRequest): Promise<GeoRule> { return put(`/sites/${siteId}/geo-rules/${ruleId}`, data) }
export function deleteGeoRule(siteId: string, ruleId: string): Promise<{ deleted: boolean }> { return del(`/sites/${siteId}/geo-rules/${ruleId}`) }
export function reorderGeoRules(siteId: string, ruleIds: string[]): Promise<GeoRule[]> { return put(`/sites/${siteId}/geo-rules/order`, { rule_ids: ruleIds }) }
