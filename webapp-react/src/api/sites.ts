import { del, get, post, put } from './client'
import type { CreateSiteRequest, PaginatedData, SiteDetail, SiteListItem } from './types'

export interface SitesQueryParams {
  page?: number
  page_size?: number
  keyword?: string
  status?: string
}

export function getSites(params?: SitesQueryParams): Promise<PaginatedData<SiteListItem>> {
  return get('/sites', { params })
}

export function createSite(data: CreateSiteRequest): Promise<{
  site_id: string
  operation_id: string
  status: string
}> {
  return post('/sites', data)
}

export function getSite(siteId: string): Promise<SiteDetail> {
  return get(`/sites/${siteId}`)
}

export function updateSite(
  siteId: string,
  data: Record<string, unknown>
): Promise<{
  site_id: string
  operation_id: string
}> {
  return put(`/sites/${siteId}`, data)
}

export function updateSiteDocument(
  siteId: string,
  data: {
    index_files: string[]
    autoindex_enabled: boolean
    autoindex_exact_size?: boolean
    autoindex_localtime?: boolean
    autoindex_format?: string
    error_page_404?: string
    error_page_403?: string
    expected_file_hash?: string
  }
): Promise<{ site_id: string; operation_id: string }> {
  return put(`/sites/${siteId}/document`, data)
}

export function enableSite(siteId: string): Promise<{ status: string; operation_id: string }> {
  return post(`/sites/${siteId}/enable`, {})
}

export function disableSite(siteId: string): Promise<{ status: string; operation_id: string }> {
  return post(`/sites/${siteId}/disable`, {})
}

export function deleteSite(
  siteId: string,
  data: {
    delete_root?: boolean
    delete_logs?: boolean
    delete_ssl_files?: boolean
    confirm_primary_domain?: string
  }
): Promise<{ deleted: boolean; operation_id: string }> {
  return del(`/sites/${siteId}`, { data })
}

export interface ImportScanItem {
  source_file: string
  server_names: string[]
  listen: string[]
  root_path: string
  access_log_path?: string
  error_log_path?: string
  config_path_ok: boolean
  root_path_ok: boolean
  access_log_path_ok: boolean
  error_log_path_ok: boolean
  warnings?: string[]
}

export function importScan(): Promise<{ items: ImportScanItem[] }> {
  return post('/sites/import-scan', {})
}

export function importSite(sourceFile: string): Promise<{
  site_id: string
  operation_id: string
  status: string
}> {
  return post('/sites/import', { source_file: sourceFile })
}
