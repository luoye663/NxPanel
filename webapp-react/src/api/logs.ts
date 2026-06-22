import { del, get, http, post } from './client'
import type { LoginAuditItem, PaginatedData, ServiceLogResponse, TaskLogTypeList } from './types'

export function getLogs(
  siteId: string,
  params: { type: 'access' | 'error'; lines?: number }
): Promise<{
  type: string
  path: string
  lines: string[]
  truncated: boolean
  max_bytes: number
}> {
  return get(`/sites/${siteId}/logs`, { params })
}

export function truncateLog(
  siteId: string,
  data: { type: 'access' | 'error'; confirm: boolean }
): Promise<{ truncated: boolean; operation_id: string }> {
  return post(`/sites/${siteId}/logs/truncate`, data)
}

export interface LogSearchResponse {
  type: string
  path: string
  lines: string[]
  matched: number
  truncated: boolean
  max_bytes: number
}

export interface RotatedLogItem {
  name: string
  path: string
  size: number
  mod_time: string
  compressed: boolean
}

export function searchLogs(siteId: string, params: { type: 'access' | 'error'; q: string; lines?: number; rotated?: string }): Promise<LogSearchResponse> {
  return get(`/sites/${siteId}/logs/search`, { params })
}

export function getRotatedLogs(siteId: string, params: { type: 'access' | 'error' }): Promise<{ items: RotatedLogItem[] }> {
  return get(`/sites/${siteId}/logs/rotated`, { params })
}

export function getRotatedLogTail(siteId: string, params: { type: 'access' | 'error'; name: string; lines?: number }): ReturnType<typeof getLogs> {
  return get(`/sites/${siteId}/logs/rotated/tail`, { params })
}

export function deleteRotatedLog(siteId: string, params: { type: 'access' | 'error'; name: string }): Promise<{ ok: boolean }> {
  return del(`/sites/${siteId}/logs/rotated`, { params })
}

export function downloadLog(siteId: string, params: { type: 'access' | 'error'; rotated?: string }) {
  return http.get(`/sites/${siteId}/logs/download`, { params, responseType: 'blob' })
}

export interface LoginAuditQueryParams {
  page?: number
  page_size?: number
}

export function getLoginAudit(params?: LoginAuditQueryParams): Promise<PaginatedData<LoginAuditItem>> {
  return get('/auth/login-audit', { params })
}

export function clearLoginAudit(): Promise<{ ok: boolean }> {
  return del('/auth/login-audit')
}

export function getServiceLog(params: { service: string; lines?: number }): Promise<ServiceLogResponse> {
  return get('/system/service-logs', { params })
}

export function clearServiceLog(params: { service: string }): Promise<{ ok: boolean }> {
  return del('/system/service-logs', { params })
}

export function getTaskLogTypes(): Promise<TaskLogTypeList> {
  return get('/task-logs/types')
}

export function getTaskLog(params: { task: string; lines?: number }): Promise<ServiceLogResponse> {
  return get('/task-logs', { params })
}

export function clearTaskLog(params: { task: string }): Promise<{ ok: boolean }> {
  return del('/task-logs', { params })
}
