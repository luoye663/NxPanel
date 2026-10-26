import { del, get, post } from './client'

export interface CertificateItem {
  source: 'panel' | 'external'
  associated_sites: { site_id: string; primary_domain: string }[]
  id: string
  name: string
  domains: string[]
  issuer: string
  subject: string
  not_before: string
  not_after: string
  cert_sha256: string
  cert_path: string
  key_path: string
  created_at: string
}

export interface CertificateScan {
  certificate_id?: string
  adoption_status: 'unadopted' | 'adopted' | 'changed' | 'unknown'
  site_id: string
  primary_domain: string
  file_hash: string
  server_hash?: string
  cert_path?: string
  key_path?: string
  read_only_reason?: string
  certificate?: { subject: string; issuer: string; not_after: string; dns_names: string[] }
}

export function scanSiteCertificate(siteId: string): Promise<CertificateScan> {
  return get(`/sites/${siteId}/certificates/scan`)
}

export function adoptSiteCertificate(siteId: string, expectedHash: string): Promise<{ certificate_id: string; operation_id: string }> {
  return post('/certificates/adopt', { site_id: siteId, expected_hash: expectedHash })
}

export interface CertificateReplacementPreview {
  matched_domains: string[]
  unmatched_domains: string[]
  dependency_hash: string
  file_hash: string
  certificate_sha256: string
  old_cert_path: string
  new_cert_path: string
  old_key_path: string
  new_key_path: string
  not_after: string
}
export function previewCertificateReplacement(siteId: string, certificateId: string): Promise<CertificateReplacementPreview> {
  return post(`/sites/${siteId}/certificates/replace-preview`, { certificate_id: certificateId })
}
export function replaceSiteCertificate(siteId: string, certificateId: string, preview: CertificateReplacementPreview): Promise<{ operation_id: string }> {
  return post(`/sites/${siteId}/certificates/replace`, { certificate_id: certificateId, expected_hash: preview.file_hash, certificate_sha256: preview.certificate_sha256, dependency_hash: preview.dependency_hash })
}

export function listCertificates(): Promise<CertificateItem[]> {
  return get('/certificates')
}

export function uploadCertificate(data: { name: string; certificate_pem: string; private_key_pem: string }): Promise<{ certificate: CertificateItem; operation_id: string }> {
  return post('/certificates', data)
}

export function deleteCertificate(certId: string): Promise<{ operation_id: string }> {
  return del(`/certificates/${certId}`)
}

export function deployCertificate(certId: string, data: { site_id: string; force_https: boolean; hsts_enabled?: boolean }): Promise<{ ssl: { enabled: boolean; mode: string; cert_path: string; not_after: string; dns_names: string[] }; operation_id: string }> {
  return post(`/certificates/${certId}/deploy`, data)
}
