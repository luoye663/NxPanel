import { del, get, http, put } from './client'

export interface SSLStatus {
  enabled: boolean
  mode: string
  cert_path: string
  key_path: string
  issuer: string
  subject: string
  not_before: string
  not_after: string
  dns_names: string[]
  force_https: boolean
  hsts_enabled: boolean
}

export interface ManualPemRequest {
  certificate_pem: string
  private_key_pem: string
  force_https: boolean
  hsts_enabled?: boolean
}

export interface ExistingFilesRequest {
  cert_path: string
  key_path: string
  force_https: boolean
  hsts_enabled?: boolean
}

export function getSSL(siteId: string): Promise<SSLStatus> {
  return get(`/sites/${siteId}/ssl`)
}

export function uploadManualPem(siteId: string, data: ManualPemRequest): Promise<{ ssl: Partial<SSLStatus>; operation_id: string }> {
  return put(`/sites/${siteId}/ssl/manual-pem`, data)
}

export function useExistingFiles(siteId: string, data: ExistingFilesRequest): Promise<{ ssl: Partial<SSLStatus>; operation_id: string }> {
  return put(`/sites/${siteId}/ssl/existing-files`, data)
}

export function disableSSL(siteId: string, data?: { delete_managed_ssl_files?: boolean }): Promise<{ ssl: Partial<SSLStatus>; operation_id: string }> {
  return del(`/sites/${siteId}/ssl`, { data: data || {} })
}

export function getSSLContent(siteId: string): Promise<{ certificate_pem: string; private_key_pem: string }> {
  return get(`/sites/${siteId}/ssl/content`)
}

export function downloadSSLCertificate(siteId: string): Promise<Blob> {
  return http.get(`/sites/${siteId}/ssl/download`, { responseType: 'blob' }).then((response: { data: Blob }) => response.data)
}
