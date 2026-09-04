import { ApiError, del, get, post, put } from './client'

export type PluginState = 'installed' | 'enabled' | 'disabled' | 'error' | 'installing' | 'updating'
export type PluginHealth = 'healthy' | 'degraded' | 'unhealthy' | 'unknown'

export interface PluginPermission {
  name: string
  label?: string
  description?: string
  risk?: 'low' | 'medium' | 'high'
}

export interface PluginCatalogItem {
  id: string
  name: string
  summary: string
  description?: string
  publisher: string
  version: string
  installed_version?: string
  icon?: string
  state?: PluginState
  health?: PluginHealth
  compatible: boolean
  incompatibility_reason?: string
  update_available?: boolean
  permissions: PluginPermission[]
  required_providers?: string[]
  access?: 'free' | 'licensed'
  source?: 'official' | 'developer'
}

export interface PluginInstallation extends PluginCatalogItem {
  enabled: boolean
  installed_at?: string
  updated_at?: string
  last_error?: string
  approved_permissions?: string[]
  source: 'official' | 'developer'
  verification_status: 'tuf_verified' | 'developer_unverified'
  developer_mode_required?: boolean
  source_conflict?: boolean
  authorization_id?: string
}

export interface PluginRepositoryStatus {
  configured: boolean
  available?: boolean
  using_cached_metadata?: boolean
  metadata_expires_at?: string
  last_refreshed_at?: string
  last_error?: string
}

interface PluginRepositoryStatusWire {
  configured: boolean
  using_cache?: boolean
  catalog_version?: number
  last_refresh_at?: string
  last_refresh_error?: string
}

export interface PluginDeveloperMode {
  enabled: boolean
  running_developer_plugins?: number
}

export interface DeveloperPackageInspection {
  upload_token: string
  expires_at?: string
  filename: string
  package_sha256: string
  id: string
  name: string
  publisher: string
  version: string
  summary?: string
  permissions: PluginPermission[]
  network_domains?: string[]
  required_providers?: string[]
  conflicts?: string[]
}

interface DeveloperPackageInspectionWire {
  upload_token: string
  expires_at?: string
  filename: string
  package_sha256: string
  manifest: {
    id: string
    name: string
    publisher: string
    version: string
    permissions?: string[]
    network_domains?: string[]
    providers?: string[]
  }
}

export type PluginAuthorizationStatus = 'active' | 'reauthorization_required'

export interface PluginAuthorizationSummary {
  authorization_id: string
  account_id: string
  display_name: string
  email_masked?: string
  status: PluginAuthorizationStatus
  access_expires_at?: string
  refresh_expires_at?: string
  last_used_at?: string
}

export interface PluginAuthorizationChallenge {
  attempt_id: string
  user_code: string
  verification_uri: string
  verification_uri_complete?: string
  expires_at: string
  interval: number
}

export type PluginAuthorizationPollStatus = 'pending' | 'slow_down' | 'authorized' | 'denied' | 'expired'

export interface PluginAuthorizationPollResult {
  status: PluginAuthorizationPollStatus
  authorization?: PluginAuthorizationSummary
  message?: string
  interval?: number
}

export interface PluginAuthorizationRequiredDetails {
  plugin_id?: string
  version?: string
  authorizations?: PluginAuthorizationSummary[]
}

export type PluginEntitlementDeniedReason = 'not_granted' | 'expired' | 'instance_limit_reached' | 'account_disabled'

export interface PluginEntitlementDeniedDetails extends PluginAuthorizationRequiredDetails {
  reason?: PluginEntitlementDeniedReason
}

export type PluginInstallAuthorizationIssue =
  | { kind: 'authorization_required'; details: PluginAuthorizationRequiredDetails }
  | { kind: 'entitlement_denied'; details: PluginEntitlementDeniedDetails }

export function getPluginInstallAuthorizationIssue(error: unknown): PluginInstallAuthorizationIssue | null {
  if (!(error instanceof ApiError)) return null
  if (error.code === 'PLUGIN_AUTHORIZATION_REQUIRED' && error.status === 409) {
    return { kind: 'authorization_required', details: (error.details || {}) as PluginAuthorizationRequiredDetails }
  }
  if (error.code === 'PLUGIN_ENTITLEMENT_DENIED' && error.status === 403) {
    return { kind: 'entitlement_denied', details: (error.details || {}) as PluginEntitlementDeniedDetails }
  }
  return null
}

export interface PluginOperation {
  operation_id: string
  status?: string
}

export type PluginContributionPoint = 'global_page' | 'site_detail_tab'

export interface PluginContribution {
  id: string
  plugin_id: string
  point: PluginContributionPoint
  label: string
  description?: string
  icon?: string
  route?: string
  ui_entry?: string
  renderer?: 'iframe'
  rpc_methods?: string[]
}

export interface PluginRPCRequest {
  method: string
  payload?: unknown
  context?: Record<string, unknown>
}

function asItems<T>(value: { items?: T[] } | T[]): T[] {
  return Array.isArray(value) ? value : value.items || []
}

export async function getPluginCatalog(): Promise<PluginCatalogItem[]> {
  return asItems(await get<{ items?: PluginCatalogItem[] } | PluginCatalogItem[]>('/plugins/catalog'))
}

export function refreshPluginCatalog(): Promise<PluginOperation | { refreshed: boolean }> {
  return post('/plugins/catalog/refresh')
}

export async function getPluginRepositoryStatus(): Promise<PluginRepositoryStatus> {
  const status = await get<PluginRepositoryStatusWire>('/plugins/repository/status')
  return {
    configured: status.configured,
    available: status.configured && !status.last_refresh_error,
    using_cached_metadata: status.using_cache,
    last_refreshed_at: status.last_refresh_at,
    last_error: status.last_refresh_error,
  }
}

export function getPluginDeveloperMode(): Promise<PluginDeveloperMode> {
  return get('/plugins/developer-mode')
}

export function setPluginDeveloperMode(enabled: boolean, acknowledgeRisk: boolean): Promise<PluginDeveloperMode> {
  return put('/plugins/developer-mode', { enabled, acknowledge_risk: acknowledgeRisk })
}

export async function inspectDeveloperPackage(file: File): Promise<DeveloperPackageInspection> {
  const body = new FormData()
  body.append('file', file)
  const inspected = await post<DeveloperPackageInspectionWire>('/plugins/developer/packages/inspect', body, { timeout: 60_000 })
  return {
    upload_token: inspected.upload_token,
    expires_at: inspected.expires_at,
    filename: inspected.filename,
    package_sha256: inspected.package_sha256,
    id: inspected.manifest.id,
    name: inspected.manifest.name,
    publisher: inspected.manifest.publisher,
    version: inspected.manifest.version,
    permissions: (inspected.manifest.permissions || []).map((name) => ({ name })),
    network_domains: inspected.manifest.network_domains || [],
    required_providers: inspected.manifest.providers || [],
    conflicts: [],
  }
}

export function installDeveloperPackage(uploadToken: string, approvedPermissions: string[]): Promise<PluginOperation> {
  return post('/plugins/developer/packages/install', { upload_token: uploadToken, approved_permissions: approvedPermissions })
}

export async function getPluginAuthorizations(): Promise<PluginAuthorizationSummary[]> {
  return asItems(await get<{ items?: PluginAuthorizationSummary[] } | PluginAuthorizationSummary[]>('/plugins/authorizations'))
}

export function createPluginAuthorizationDevice(pluginId: string, version: string): Promise<PluginAuthorizationChallenge> {
  return post('/plugins/authorizations/device', { plugin_id: pluginId, version })
}

export function pollPluginAuthorizationDevice(attemptId: string): Promise<PluginAuthorizationPollResult> {
  return post(`/plugins/authorizations/device/${encodeURIComponent(attemptId)}/poll`)
}

export function bindPluginAuthorization(pluginId: string, authorizationId: string): Promise<PluginAuthorizationSummary> {
  return put(`/plugins/${encodeURIComponent(pluginId)}/authorization`, { authorization_id: authorizationId })
}

export function deletePluginAuthorization(authorizationId: string): Promise<void> {
  return del(`/plugins/authorizations/${encodeURIComponent(authorizationId)}`)
}

export async function getPlugins(): Promise<PluginInstallation[]> {
  return asItems(await get<{ items?: PluginInstallation[] } | PluginInstallation[]>('/plugins'))
}

export function getPlugin(pluginId: string): Promise<PluginInstallation> {
  return get(`/plugins/${encodeURIComponent(pluginId)}`)
}

export function installPlugin(pluginId: string, version: string, permissions: string[]): Promise<PluginOperation> {
  return post(`/plugins/${encodeURIComponent(pluginId)}/install`, { version, approved_permissions: permissions })
}

export function updatePlugin(pluginId: string, version: string, permissions: string[]): Promise<PluginOperation> {
  return post(`/plugins/${encodeURIComponent(pluginId)}/update`, { version, approved_permissions: permissions })
}

export function setPluginEnabled(pluginId: string, enabled: boolean): Promise<PluginOperation> {
  return post(`/plugins/${encodeURIComponent(pluginId)}/${enabled ? 'enable' : 'disable'}`)
}

export function uninstallPlugin(pluginId: string, purgeData = false): Promise<PluginOperation> {
  return del(`/plugins/${encodeURIComponent(pluginId)}`, { params: { purge_data: purgeData } })
}

export function callPluginRPC<T>(pluginId: string, request: PluginRPCRequest): Promise<T> {
  return post(`/plugins/${encodeURIComponent(pluginId)}/rpc`, request)
}

export async function getPluginContributions(): Promise<PluginContribution[]> {
  return asItems(await get<{ items?: PluginContribution[] } | PluginContribution[]>('/plugins/contributions'))
}
