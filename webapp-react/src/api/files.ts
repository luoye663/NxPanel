import { get, http, post } from './client'
import { withGatePrefix } from './gate'
import type { FileListResponse, FileReadResponse } from './types'

export interface FileRootsResponse {
  roots: string[]
}

export interface FileWriteResponse {
  success: boolean
}

export interface FileUploadResponse {
  success: boolean
  path: string
  size: number
}

export function getFilesRoots(): Promise<FileRootsResponse> {
  return get('/files/roots')
}

export function globalListFiles(path: string): Promise<FileListResponse> {
  return get('/files/list', { params: { path } })
}

export function globalReadFile(path: string): Promise<FileReadResponse> {
  return get('/files/read', { params: { path } })
}

export function globalWriteFile(path: string, contentBase64: string): Promise<FileWriteResponse> {
  return post('/files/write', { path, content_base64: contentBase64 })
}

export function globalRemoveFiles(paths: string[]): Promise<FileWriteResponse> {
  return post('/files/remove', { paths })
}

export function globalMkdir(path: string): Promise<FileWriteResponse> {
  return post('/files/mkdir', { path })
}

export function globalMoveFile(source: string, destination: string): Promise<FileWriteResponse> {
  return post('/files/move', { source, destination })
}

export function globalCopyFiles(paths: string[], destDir: string): Promise<FileWriteResponse> {
  return post('/files/copy', { paths, dest_dir: destDir })
}

export function globalChmod(path: string, mode: string, recursive: boolean): Promise<FileWriteResponse> {
  return post('/files/chmod', { path, mode, recursive })
}

export function globalChown(path: string, owner: string, group: string, recursive: boolean): Promise<FileWriteResponse> {
  return post('/files/chown', { path, owner, group, recursive })
}

export function globalCompress(paths: string[], outputPath: string, format: string): Promise<FileWriteResponse & { output_path: string }> {
  return post('/files/compress', { paths, output_path: outputPath, format })
}

export function globalExtract(archivePath: string, destDir: string): Promise<FileWriteResponse & { dest_dir: string }> {
  return post('/files/extract', { archive_path: archivePath, dest_dir: destDir })
}

export function globalGetDownloadUrl(filePath: string): string {
  return withGatePrefix(`/files/download?path=${encodeURIComponent(filePath)}`)
}

export function globalGetArchiveUrl(paths: string[]): string {
  return withGatePrefix(`/files/archive?paths=${paths.map((path) => encodeURIComponent(path)).join(',')}`)
}

export function globalUploadFile(targetPath: string, file: File, onProgress?: (percent: number) => void, signal?: AbortSignal): Promise<FileUploadResponse> {
  const formData = new FormData()
  formData.append('file', file)
  formData.append('path', targetPath)

  return http.post<FileUploadResponse>('/files/upload', formData, {
    headers: { 'Content-Type': 'multipart/form-data' },
    timeout: 5 * 60 * 1000,
    signal,
    onUploadProgress: (event) => {
      if (event.total && onProgress) onProgress(Math.round((event.loaded / event.total) * 100))
    },
  }).then((resp) => resp.data)
}

export function listFiles(siteId: string, path: string): Promise<FileListResponse> {
  return get(`/sites/${siteId}/files`, { params: path ? { path } : {} })
}

export function readFile(siteId: string, path: string): Promise<FileReadResponse> {
  return get(`/sites/${siteId}/files/read`, { params: { path } })
}

export function writeFile(siteId: string, path: string, contentBase64: string): Promise<FileWriteResponse> {
  return post(`/sites/${siteId}/files/write`, { path, content_base64: contentBase64 })
}

export function removeFiles(siteId: string, paths: string[]): Promise<FileWriteResponse> {
  return post(`/sites/${siteId}/files/remove`, { paths })
}

export function mkdir(siteId: string, path: string): Promise<FileWriteResponse> {
  return post(`/sites/${siteId}/files/mkdir`, { path })
}

export function moveFile(siteId: string, source: string, destination: string): Promise<FileWriteResponse> {
  return post(`/sites/${siteId}/files/move`, { source, destination })
}

export function copyFiles(siteId: string, paths: string[], destDir: string): Promise<FileWriteResponse> {
  return post(`/sites/${siteId}/files/copy`, { paths, dest_dir: destDir })
}

export function chmod(siteId: string, path: string, mode: string, recursive: boolean): Promise<FileWriteResponse> {
  return post(`/sites/${siteId}/files/chmod`, { path, mode, recursive })
}

export function chown(siteId: string, path: string, owner: string, group: string, recursive: boolean): Promise<FileWriteResponse> {
  return post(`/sites/${siteId}/files/chown`, { path, owner, group, recursive })
}

export function compress(siteId: string, paths: string[], outputPath: string, format: string): Promise<FileWriteResponse & { output_path: string }> {
  return post(`/sites/${siteId}/files/compress`, { paths, output_path: outputPath, format })
}

export function extract(siteId: string, archivePath: string, destDir: string): Promise<FileWriteResponse & { dest_dir: string }> {
  return post(`/sites/${siteId}/files/extract`, { archive_path: archivePath, dest_dir: destDir })
}

export function getDownloadUrl(siteId: string, filePath: string): string {
  return withGatePrefix(`/sites/${siteId}/files/download?path=${encodeURIComponent(filePath)}`)
}

export function getArchiveUrl(siteId: string, paths: string[]): string {
  return withGatePrefix(`/sites/${siteId}/files/archive?paths=${paths.map((path) => encodeURIComponent(path)).join(',')}`)
}

export function uploadFile(siteId: string, targetPath: string, file: File, onProgress?: (percent: number) => void, signal?: AbortSignal): Promise<FileUploadResponse> {
  const formData = new FormData()
  formData.append('file', file)
  formData.append('path', targetPath)

  return http.post<FileUploadResponse>(`/sites/${siteId}/files/upload`, formData, {
    headers: { 'Content-Type': 'multipart/form-data' },
    timeout: 5 * 60 * 1000,
    signal,
    onUploadProgress: (event) => {
      if (event.total && onProgress) onProgress(Math.round((event.loaded / event.total) * 100))
    },
  }).then((resp) => resp.data)
}
