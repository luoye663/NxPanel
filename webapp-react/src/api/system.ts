import { get, post, put } from './client'
import { withGatePrefix } from './gate'
import type { SystemOverview, UpgradeStatus } from './types'

export function getSystemOverview(): Promise<SystemOverview> {
  return get('/system/overview')
}

export function getUpgradeStatus(): Promise<UpgradeStatus> {
  return get('/system/upgrade', { timeout: 5000 })
}

// triggerUpgradeCheck 手动触发一次升级检查，返回最新状态。
// 后端内置 60 秒冷却，冷却内重复调用直接返回缓存状态。
export function triggerUpgradeCheck(): Promise<UpgradeStatus> {
  return post('/system/upgrade/check')
}

export function setUpgradeChannel(channel: UpgradeStatus['channel']): Promise<UpgradeStatus> {
  return put('/system/upgrade/channel', { channel })
}

export function prepareUpgrade(version: string): Promise<UpgradeStatus> {
  return post('/system/upgrade/prepare', { version })
}

export function cancelUpgrade(id: string): Promise<UpgradeStatus> {
  return post('/system/upgrade/cancel', { id })
}

export function activateUpgrade(id: string): Promise<{ id: string; version: string }> {
  return post('/system/upgrade/activate', { id })
}

export function systemMetricsStreamURL(scope?: string): string {
  const query = scope ? `?scope=${encodeURIComponent(scope)}` : ''
  // SSE 使用同源 URL，继续依赖浏览器携带 session cookie。
  return withGatePrefix(`/system/metrics/stream${query}`)
}
