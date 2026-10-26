import { get, put } from './client'

export interface MaintenanceStatus {
  site_id: string
  maintenance_enabled: boolean
  desired_enabled: boolean
  html_override: string | null
  version: number
  apply_status: 'synced' | 'pending'
  operation_id: string
  file_hash: string
  effective_html: string
  inherits: boolean
  drifted: boolean
  read_only_reason?: string
}
export interface MaintenanceDefaults { html: string | null; version: number; effective_html: string; builtin_html: string }
export function getMaintenance(id: string): Promise<MaintenanceStatus> { return get(`/sites/${id}/maintenance`) }
export function updateMaintenance(id: string, data: { enabled: boolean; html_override: string | null; version: number; expected_hash: string }): Promise<{ operation_id: string }> { return put(`/sites/${id}/maintenance`, data) }
export function getMaintenanceDefaults(): Promise<MaintenanceDefaults> { return get('/settings/maintenance') }
export function saveMaintenanceDefaults(data: { html: string | null; version: number }): Promise<{ operation_id: string }> { return put('/settings/maintenance', data) }
