import { get, put } from './client'
import type { MarkerStatus } from './types'

export interface SiteConfigResponse {
  content: string
  hash: string
  is_imported: boolean
  marker_status: MarkerStatus
  required_markers: string[]
}

export interface SaveSiteConfigRequest {
  content: string
  expected_hash?: string
  danger_confirmed: boolean
}

export interface SaveSiteConfigResponse {
  hash: string
  marker_status: MarkerStatus
  sync_warnings: string[]
  operation_id: string
}

// 完整配置编辑直接写站点 conf，必须由调用方做危险确认并携带 hash 防漂移。
export function getSiteConfig(siteId: string): Promise<SiteConfigResponse> {
  return get(`/sites/${siteId}/config`)
}

export function saveSiteConfig(siteId: string, data: SaveSiteConfigRequest): Promise<SaveSiteConfigResponse> {
  return put(`/sites/${siteId}/config`, data)
}
