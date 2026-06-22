import { useMemo } from 'react'
import {
  chmod,
  chown,
  compress,
  copyFiles,
  extract,
  getArchiveUrl,
  getDownloadUrl,
  globalChmod,
  globalChown,
  globalCompress,
  globalCopyFiles,
  globalExtract,
  globalGetArchiveUrl,
  globalGetDownloadUrl,
  globalListFiles,
  globalMkdir,
  globalMoveFile,
  globalReadFile,
  globalRemoveFiles,
  globalUploadFile,
  globalWriteFile,
  readFile,
  listFiles,
  mkdir,
  moveFile,
  removeFiles,
  uploadFile,
  writeFile,
  type FileUploadResponse,
} from '@/api/files'
import type { FileListResponse, FileReadResponse } from '@/api/types'

export interface FileApiBridge {
  isGlobal: boolean
  list: (path: string) => Promise<FileListResponse>
  read: (path: string) => Promise<FileReadResponse>
  writeEmpty: (path: string) => Promise<unknown>
  write: (path: string, contentBase64: string) => Promise<unknown>
  mkdir: (path: string) => Promise<unknown>
  remove: (paths: string[]) => Promise<unknown>
  move: (source: string, destination: string) => Promise<unknown>
  copy: (paths: string[], destDir: string) => Promise<unknown>
  chmod: (path: string, mode: string, recursive: boolean) => Promise<unknown>
  chown: (path: string, owner: string, group: string, recursive: boolean) => Promise<unknown>
  compress: (paths: string[], outputPath: string, format: string) => Promise<unknown>
  extract: (archivePath: string, destDir: string) => Promise<unknown>
  upload: (targetPath: string, file: File, onProgress?: (percent: number) => void, signal?: AbortSignal) => Promise<FileUploadResponse>
  downloadUrl: (path: string) => string
  archiveUrl: (paths: string[]) => string
}

export function useFileApi(siteId?: string): FileApiBridge {
  return useMemo(() => {
    const isGlobal = !siteId

    // 同一个文件管理器同时服务全局文件页和站点详情文件页，所有 API 差异集中在这里。
    if (isGlobal) {
      return {
        isGlobal,
        list: globalListFiles,
        read: globalReadFile,
        writeEmpty: (path: string) => globalWriteFile(path, ''),
        write: globalWriteFile,
        mkdir: globalMkdir,
        remove: globalRemoveFiles,
        move: globalMoveFile,
        copy: globalCopyFiles,
        chmod: globalChmod,
        chown: globalChown,
        compress: globalCompress,
        extract: globalExtract,
        upload: globalUploadFile,
        downloadUrl: globalGetDownloadUrl,
        archiveUrl: globalGetArchiveUrl,
      }
    }

    return {
      isGlobal,
      list: (path: string) => listFiles(siteId, path),
      read: (path: string) => readFile(siteId, path),
      writeEmpty: (path: string) => writeFile(siteId, path, ''),
      write: (path: string, contentBase64: string) => writeFile(siteId, path, contentBase64),
      mkdir: (path: string) => mkdir(siteId, path),
      remove: (paths: string[]) => removeFiles(siteId, paths),
      move: (source: string, destination: string) => moveFile(siteId, source, destination),
      copy: (paths: string[], destDir: string) => copyFiles(siteId, paths, destDir),
      chmod: (path: string, mode: string, recursive: boolean) => chmod(siteId, path, mode, recursive),
      chown: (path: string, owner: string, group: string, recursive: boolean) => chown(siteId, path, owner, group, recursive),
      compress: (paths: string[], outputPath: string, format: string) => compress(siteId, paths, outputPath, format),
      extract: (archivePath: string, destDir: string) => extract(siteId, archivePath, destDir),
      upload: (targetPath: string, file: File, onProgress?: (percent: number) => void, signal?: AbortSignal) => uploadFile(siteId, targetPath, file, onProgress, signal),
      downloadUrl: (path: string) => getDownloadUrl(siteId, path),
      archiveUrl: (paths: string[]) => getArchiveUrl(siteId, paths),
    }
  }, [siteId])
}
