import { get, put } from './client'
import type { DefaultPagesSettings, DefaultSiteSettings, HTTPSHijackSettings, LogRotateSettings, SecuritySettings, UpdateLogRotateRequest, UpdateSecuritySettingsRequest } from './types'

export function getDefaultPages(): Promise<DefaultPagesSettings> {
  return get('/settings/default-pages')
}

export function updateDefaultPages(data: Partial<DefaultPagesSettings>): Promise<DefaultPagesSettings> {
  return put('/settings/default-pages', data)
}

export function getDefaultSite(): Promise<DefaultSiteSettings> {
  return get('/settings/default-site')
}

export function updateDefaultSite(data: { site_id: string }): Promise<DefaultSiteSettings> {
  return put('/settings/default-site', data)
}

export function getHTTPSHijack(): Promise<HTTPSHijackSettings> {
  return get('/settings/https-hijack')
}

export function updateHTTPSHijack(data: {
  enabled: boolean
  return_status_code: number
  cert_mode: string
  custom_cert_id?: string
}): Promise<HTTPSHijackSettings> {
  return put('/settings/https-hijack', data)
}

export function getLogRotation(): Promise<LogRotateSettings> {
  return get('/settings/log-rotation')
}

export function updateLogRotation(data: UpdateLogRotateRequest): Promise<LogRotateSettings> {
  return put('/settings/log-rotation', data)
}

export function getSecuritySettings(): Promise<SecuritySettings> {
  return get('/settings/security')
}

export function updateSecuritySettings(data: UpdateSecuritySettingsRequest): Promise<SecuritySettings> {
  return put('/settings/security', data)
}
