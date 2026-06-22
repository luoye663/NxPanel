import { del, get, post } from './client'

export interface CertificateItem {
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
