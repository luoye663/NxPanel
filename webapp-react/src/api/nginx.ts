import { get, post, put } from './client'
import type { NginxConfResponse, NginxParametersResponse, SaveNginxConfResponse, SaveNginxParametersResponse } from './types'

export interface NginxDetectResponse {
  bin: string
  version: string
  conf_path: string
  prefix: string
  test_ok: boolean
  stderr: string
  capabilities: Record<'geo' | 'map' | 'realip', boolean>
}

export interface NginxTestResponse {
  ok: boolean
  stdout: string
  stderr: string
}

export interface NginxReloadResponse {
  ok: boolean
  operation_id: string
}

export interface NginxEnsureIncludeResponse {
  installed: boolean
  changed: boolean
  entry_file: string
  operation_id: string
}

export function detectNginx(): Promise<NginxDetectResponse> {
  return post('/nginx/detect', {})
}

export function testNginx(): Promise<NginxTestResponse> {
  return post('/nginx/test')
}

export function reloadNginx(data?: { test_before_reload?: boolean }): Promise<NginxReloadResponse> {
  return post('/nginx/reload', data || { test_before_reload: true })
}

export function ensureInclude(data?: { confirm_modify_main_conf?: boolean }): Promise<NginxEnsureIncludeResponse> {
  return post('/nginx/include/ensure', data || { confirm_modify_main_conf: false })
}

export function getNginxConf(): Promise<NginxConfResponse> {
  return get('/nginx/conf')
}

export function saveNginxConf(data: { content: string; expected_hash: string; danger_confirmed: boolean }): Promise<SaveNginxConfResponse> {
  return put('/nginx/conf', data)
}

export function getNginxParameters(): Promise<NginxParametersResponse> {
  return get('/nginx/parameters')
}

export function saveNginxParameters(data: { parameters: Record<string, string> }): Promise<SaveNginxParametersResponse> {
  return put('/nginx/parameters', data)
}

export interface NginxRuntimeStatus {
  running: boolean
  master_pid: number
  workers: number
  processes: number
  memory_rss: number
  memory_pss?: number | null
  uptime_seconds: number
  instances: { pid: number; conf_path?: string; match: 'matching' | 'other' | 'unknown' }[]
  blocked_reason?: string
  other_actions_blocked?: boolean
  restart_generation?: string
}

export type NginxControlAction = 'start' | 'stop' | 'restart'

export function getNginxStatus(): Promise<NginxRuntimeStatus> {
  return get('/nginx/status')
}

export function controlNginx(action: NginxControlAction, restartGeneration?: string): Promise<NginxReloadResponse> {
  return post(`/nginx/${action}`, { restart_generation: restartGeneration })
}

export interface NginxModule {
  id: string
  name: string
  description?: string
  state: 'available' | 'enabled' | 'configured' | 'unavailable' | 'busy' | 'drifted' | 'needs_attention'
  present: boolean
  loaded: boolean
  configured: boolean
  runtime?: string
  version?: string
  reason?: string
  action?: 'install'
  confirmation?: string
  uninstall_confirmation?: string
}

export interface NginxModuleJob {
  id: string
  module_id: string
  action: 'install' | 'uninstall'
  state: 'pending' | 'success' | 'failed' | 'needs_attention'
  message?: string
  script_detail?: string
}

export function getNginxModules(refresh = false): Promise<{
  modules: NginxModule[]
  feed_state: 'online' | 'stale' | 'error' | 'busy'
  feed_message?: string
  busy?: boolean
  test_feed?: boolean
  latest_job?: NginxModuleJob
}> {
  return get(refresh ? '/nginx/modules?refresh=1' : '/nginx/modules', { timeout: 26_000 })
}

export function installNginxModule(id: NginxModule['id'], confirmation: string): Promise<NginxModuleJob> {
  return post('/nginx/modules/install', { id, confirmation })
}

export function uninstallNginxModule(id: string, confirmation: string): Promise<NginxModuleJob> {
  return post('/nginx/modules/uninstall', { id, confirmation })
}

export function getNginxModuleJob(id: string): Promise<NginxModuleJob> {
  return get(`/nginx/modules/jobs/${encodeURIComponent(id)}`)
}
