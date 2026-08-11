import { del, get, post, put } from './client'
import type {
  NginxUpstream,
  NginxUpstreamSaveRequest,
  NginxUpstreamStatus,
  NginxUpstreamSyncResult,
  NginxUpstreamValidateResult,
  NginxUpstreamWriteResult,
} from './types'

export const upstreamKeys = {
  all: ['nginx', 'upstreams'] as const,
  status: ['nginx', 'upstreams', 'status'] as const,
}

export function listUpstreams(): Promise<NginxUpstream[]> {
  return get('/nginx/upstreams')
}

export function getUpstream(id: string): Promise<NginxUpstream> {
  return get(`/nginx/upstreams/${id}`)
}

export function createUpstream(data: NginxUpstreamSaveRequest): Promise<NginxUpstreamWriteResult> {
  return post('/nginx/upstreams', data)
}

export function updateUpstream(id: string, data: NginxUpstreamSaveRequest): Promise<NginxUpstreamWriteResult> {
  return put(`/nginx/upstreams/${id}`, data)
}

export function deleteUpstream(id: string): Promise<NginxUpstreamWriteResult> {
  return del(`/nginx/upstreams/${id}`)
}

export function validateUpstream(data: NginxUpstreamSaveRequest): Promise<NginxUpstreamValidateResult> {
  return post('/nginx/upstreams/validate', data)
}

export function syncUpstreams(): Promise<NginxUpstreamSyncResult> {
  return post('/nginx/upstreams/sync')
}

export function getUpstreamStatus(): Promise<NginxUpstreamStatus> {
  return get('/nginx/upstreams/status')
}
