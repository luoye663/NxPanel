import { del, get } from './client'
import type { OperationItem, PaginatedData } from './types'

export interface OperationsQueryParams {
  page?: number
  page_size?: number
  target_id?: string
}

export function getOperations(params?: OperationsQueryParams): Promise<PaginatedData<OperationItem>> {
  return get('/operations', { params })
}

export function getOperation(operationId: string): Promise<{
  id: string
  action: string
  status: string
  error_code: string
  error_message: string
  stderr: string
  backups: Array<{
    file_path: string
    backup_path: string
  }>
}> {
  return get(`/operations/${operationId}`)
}

export function clearOperations(): Promise<{ ok: boolean }> {
  return del('/operations')
}
