import { get, post, put } from './client'
import { withGatePrefix } from './gate'
import type {
  AccessAnalysisFormatDetectResponse,
  AccessAnalysisJob,
  AccessAnalysisScanRequest,
  AccessAnalysisScanResponse,
  AccessAnalysisSettings,
  AccessAnalysisSummaryResponse,
  AccessAnomaly,
  AccessEntry,
  AccessIPStat,
  AccessPathStat,
  PaginatedData,
} from './types'

export interface AccessAnalysisQuery {
  from?: string
  to?: string
  ip?: string
  path?: string
  method?: string
  status?: number | string
  sort?: string
  page?: number
  page_size?: number
}

const base = (siteId: string) => `/sites/${siteId}/access-analysis`

export function getAccessAnalysisSummary(siteId: string, params?: AccessAnalysisQuery): Promise<AccessAnalysisSummaryResponse> {
  return get(`${base(siteId)}/summary`, { params })
}

export function scanAccessAnalysis(siteId: string, data: AccessAnalysisScanRequest): Promise<AccessAnalysisScanResponse> {
  return post(`${base(siteId)}/scan`, data)
}

export function getAccessAnalysisJobs(siteId: string, params?: AccessAnalysisQuery): Promise<PaginatedData<AccessAnalysisJob>> {
  return get(`${base(siteId)}/jobs`, { params })
}

export function getAccessAnalysisPaths(siteId: string, params?: AccessAnalysisQuery): Promise<PaginatedData<AccessPathStat>> {
  return get(`${base(siteId)}/paths`, { params })
}

export function getAccessAnalysisIPs(siteId: string, params?: AccessAnalysisQuery): Promise<PaginatedData<AccessIPStat>> {
  return get(`${base(siteId)}/ips`, { params })
}

export function getAccessAnalysisEntries(siteId: string, params?: AccessAnalysisQuery): Promise<PaginatedData<AccessEntry>> {
  return get(`${base(siteId)}/entries`, { params })
}

export function getAccessAnalysisAnomalies(siteId: string, params?: AccessAnalysisQuery): Promise<AccessAnomaly[]> {
  return get(`${base(siteId)}/anomalies`, { params })
}

export function getAccessAnalysisSettings(siteId: string): Promise<AccessAnalysisSettings> {
  return get(`${base(siteId)}/settings`)
}

export function saveAccessAnalysisSettings(siteId: string, data: AccessAnalysisSettings): Promise<AccessAnalysisSettings> {
  return put(`${base(siteId)}/settings`, data)
}

export function detectAccessLogFormat(siteId: string, sample?: string): Promise<AccessAnalysisFormatDetectResponse> {
  return post(`${base(siteId)}/format/detect`, { sample })
}

export function testAccessLogFormat(siteId: string, pattern: string, sample: string): Promise<AccessAnalysisFormatDetectResponse> {
  return post(`${base(siteId)}/format/test`, { pattern, sample })
}

export function optimizeAccessLogFormat(siteId: string): Promise<{ recommended_conf: string; operation_id: string }> {
  return post(`${base(siteId)}/format/optimize`, {})
}

export function accessAnalysisExportURL(siteId: string, kind: 'paths' | 'ips' | 'entries', params: AccessAnalysisQuery): string {
  const search = new URLSearchParams({ kind })
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== '') search.set(key, String(value))
  })
  return withGatePrefix(`${base(siteId)}/export?${search.toString()}`)
}
