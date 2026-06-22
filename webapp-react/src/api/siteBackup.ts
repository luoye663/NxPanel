import { del, get, post, put } from '@/api/client'
import { withGatePrefix } from '@/api/gate'
import type { SiteBackupListResponse, SiteBackupCreateRequest, SiteBackupRestoreRequest, SiteBackupSchedule, SiteBackupScheduleSaveRequest, SiteBackupTask } from '@/api/types'

export function listSiteBackups(siteId: string): Promise<SiteBackupListResponse> {
  return get<SiteBackupListResponse>(`/sites/${siteId}/backups`)
}

export function createSiteBackup(siteId: string, data: SiteBackupCreateRequest): Promise<SiteBackupTask> {
  return post<SiteBackupTask>(`/sites/${siteId}/backups`, data)
}

export function restoreSiteBackup(siteId: string, backupId: string, data: SiteBackupRestoreRequest): Promise<SiteBackupTask> {
  return post<SiteBackupTask>(`/sites/${siteId}/backups/${backupId}/restore`, data)
}

export function deleteSiteBackup(siteId: string, backupId: string): Promise<{ ok: boolean }> {
  return del<{ ok: boolean }>(`/sites/${siteId}/backups/${backupId}`)
}

export function siteBackupDownloadURL(siteId: string, backupId: string): string {
  return withGatePrefix(`/sites/${siteId}/backups/${backupId}/download`)
}

export function siteBackupTaskStreamURL(siteId: string, taskId: string): string {
  return withGatePrefix(`/sites/${siteId}/backups/tasks/${taskId}/stream`)
}

export function getSiteBackupSchedule(siteId: string): Promise<SiteBackupSchedule> {
  return get<SiteBackupSchedule>(`/sites/${siteId}/backups/schedule`)
}

export function saveSiteBackupSchedule(siteId: string, data: SiteBackupScheduleSaveRequest): Promise<SiteBackupSchedule> {
  return put<SiteBackupSchedule>(`/sites/${siteId}/backups/schedule`, data)
}
