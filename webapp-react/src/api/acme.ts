import { del, get, http, post, put } from './client'
import { withGatePrefix } from './gate'

export interface ACMEOrderItem {
  id: string
  site_id: string
  domains: string[]
  challenge_type: string
  email: string
  status: 'pending' | 'processing' | 'verifying' | 'success' | 'failed' | 'pre_validation_failed'
  certificate_id: string | null
  error_type: string | null
  error_detail: string | null
  verification_url: string | null
  verification_content: string | null
  auto_renew: boolean
  expires_at: string | null
  created_at: string
}

export function applyCertificate(data: { site_id: string; domains: string[]; challenge_type: 'http-01'; email: string }): Promise<{ order_id: string }> {
  return post('/acme/apply', data)
}

export function listACMEOrders(siteId: string): Promise<ACMEOrderItem[]> {
  return get(`/sites/${siteId}/acme/orders`)
}

export function renewOrder(orderId: string): Promise<{ order_id: string }> {
  return post(`/acme/orders/${orderId}/renew`)
}

export function deleteOrder(orderId: string): Promise<{ operation_id: string }> {
  return del(`/acme/orders/${orderId}`)
}

export function forceObtainOrder(orderId: string): Promise<{ order_id: string }> {
  return post(`/acme/orders/${orderId}/force-obtain`)
}

export function deployOrder(orderId: string, data: { site_id: string; force_https: boolean }): Promise<Record<string, unknown>> {
  return post(`/acme/orders/${orderId}/deploy`, data)
}

export function setAutoRenew(orderId: string, enabled: boolean): Promise<{ auto_renew: boolean }> {
  return put(`/acme/orders/${orderId}/auto-renew`, { auto_renew: enabled })
}

export function downloadOrder(orderId: string): Promise<Blob> {
  return http.get(`/acme/orders/${orderId}/download`, { responseType: 'blob' }).then((response: { data: Blob }) => response.data)
}

export function getOrderLogSSEUrl(orderId: string): string {
  return withGatePrefix(`/acme/orders/${orderId}/log`)
}

export function listEmails(): Promise<string[]> {
  return get('/acme/emails')
}

export function saveEmail(email: string): Promise<void> {
  return post('/acme/emails', { email })
}

export function deleteEmail(email: string): Promise<void> {
  return del(`/acme/emails/${encodeURIComponent(email)}`)
}
