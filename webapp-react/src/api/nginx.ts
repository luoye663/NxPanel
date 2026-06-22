import { get, post, put } from './client'
import type { NginxConfResponse, NginxParametersResponse, SaveNginxConfResponse, SaveNginxParametersResponse } from './types'

export interface NginxDetectResponse {
  bin: string
  version: string
  conf_path: string
  prefix: string
  test_ok: boolean
  stderr: string
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
