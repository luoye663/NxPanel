import { del, get, post, put } from './client'
import type { CreateProxyRequest, SiteProxy, UpdateProxyRequest } from './types'

// 反向代理接口保持与 Vue 版一致，后端会负责写入 marker 并执行 nginx -t。
export function listProxies(siteId: string): Promise<SiteProxy[]> {
  return get(`/sites/${siteId}/proxy`)
}

export function createProxy(
  siteId: string,
  data: CreateProxyRequest
): Promise<{ proxy: SiteProxy; operation_id: string }> {
  return post(`/sites/${siteId}/proxy`, data)
}

export function updateProxy(
  siteId: string,
  proxyId: string,
  data: UpdateProxyRequest
): Promise<{ proxy: SiteProxy; operation_id: string }> {
  return put(`/sites/${siteId}/proxy/${proxyId}`, data)
}

export function deleteProxy(siteId: string, proxyId: string): Promise<{ operation_id: string }> {
  return del(`/sites/${siteId}/proxy/${proxyId}`)
}
