import { Alert, Button, Group, Modal, Stack, TextInput } from '@mantine/core'
import { useMediaQuery } from '@mantine/hooks'
import { useQuery } from '@tanstack/react-query'
import { IconAlertTriangle } from '@tabler/icons-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import type { FileEntry } from '@/api/types'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { FileCompressModal } from '@/components/files/FileCompressModal'
import { FileEditorPanel } from '@/components/files/FileEditorPanel'
import { FileExtractModal } from '@/components/files/FileExtractModal'
import { FilePermissionModal } from '@/components/files/FilePermissionModal'
import { FilePathBar } from '@/components/files/FilePathBar'
import { FileTable } from '@/components/files/FileTable'
import { FileToolbar } from '@/components/files/FileToolbar'
import { ImagePreviewModal } from '@/components/files/ImagePreviewModal'
import { UploadProgressModal } from '@/components/files/UploadProgressModal'
import { useFileApi } from '@/hooks/useFileApi'
import { useFileClipboard } from '@/hooks/useFileClipboard'
import { useFileSelection } from '@/hooks/useFileSelection'
import { useUploadQueue } from '@/hooks/useUploadQueue'
import { confirmDanger } from '@/utils/confirm'
import { showErrorModal } from '@/utils/errorModal'
import { getFileCategory, FileCategory } from '@/utils/fileType'
import { notifySuccess, notifyWarning } from '@/utils/notify'

interface FileManagerProps {
  siteId?: string
  rootPath: string
  allowedRoots?: string[]
  lockRoot?: string
  initialPath?: string
  onPathChange?: (path: string) => void
}

type PromptMode = 'file' | 'dir' | 'rename'

interface PromptState {
  mode: PromptMode
  title: string
  label: string
  value: string
  entry?: FileEntry
}

interface FileActionState {
  path: string
  name: string
}

interface PermissionState {
  entry: FileEntry
  path: string
  mode: string
  owner: string
  group: string
  recursive: boolean
  submitting: boolean
}

interface CompressState {
  outputName: string
  format: 'zip' | 'tar.gz'
  submitting: boolean
}

interface ExtractState {
  archivePath: string
  destDir: string
  submitting: boolean
}

function joinPath(base: string, name: string): string {
  return `${base.replace(/\/+$/, '')}/${name.replace(/^\/+/, '')}`
}

function normalizePath(path: string): string {
  const trimmed = path.trim().replace(/\\+/g, '/').replace(/\/+/g, '/')
  if (!trimmed) return ''
  const prefixed = trimmed.startsWith('/') ? trimmed : `/${trimmed}`
  if (prefixed === '/') return '/'
  return prefixed.replace(/\/+$/, '')
}

function resolveInitialPath(initialPath: string | undefined, rootPath: string, allowedRoots?: string[], lockRoot?: string): string {
  const effectiveRoot = normalizePath(rootPath)
  const effectiveAllowedRoots = (allowedRoots && allowedRoots.length > 0 ? allowedRoots : [rootPath]).map(normalizePath).filter(Boolean)
  const effectiveLockRoot = normalizePath(lockRoot || '')
  const candidate = normalizePath(initialPath || rootPath)
  if (!candidate) return effectiveRoot || '/'
  if (effectiveAllowedRoots.length > 0 && !effectiveAllowedRoots.some((root) => isPathWithinRoot(candidate, root))) return effectiveRoot || '/'
  if (effectiveLockRoot && !isPathWithinRoot(candidate, effectiveLockRoot)) return effectiveLockRoot || effectiveRoot || '/'
  return candidate
}

function isPathWithinRoot(path: string, root: string): boolean {
  if (root === '/') return path.startsWith('/')
  return path === root || path.startsWith(`${root}/`)
}

function sortEntries(a: FileEntry, b: FileEntry): number {
  if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1
  return a.name.localeCompare(b.name)
}

function compressDefaultExt(format: string): string {
  return format === 'tar.gz' ? '.tar.gz' : '.zip'
}

function compressTimestamp(): string {
  const now = new Date()
  const date = now.toISOString().slice(0, 10).replace(/-/g, '')
  const hour = String(now.getHours()).padStart(2, '0')
  const minute = String(now.getMinutes()).padStart(2, '0')
  return `${date}-${hour}${minute}`
}

export function FileManager({ siteId, rootPath, allowedRoots, lockRoot, initialPath, onPathChange }: FileManagerProps) {
  const fileApi = useFileApi(siteId)
  const [currentPath, setCurrentPath] = useState(() => resolveInitialPath(initialPath, rootPath, allowedRoots, lockRoot))
  const [promptState, setPromptState] = useState<PromptState | null>(null)
  const [promptSubmitting, setPromptSubmitting] = useState(false)
  const [editingFile, setEditingFile] = useState<FileActionState | null>(null)
  const [previewingImage, setPreviewingImage] = useState<FileActionState | null>(null)
  const [permissionState, setPermissionState] = useState<PermissionState | null>(null)
  const [compressState, setCompressState] = useState<CompressState | null>(null)
  const [extractState, setExtractState] = useState<ExtractState | null>(null)
  const uploadInputRef = useRef<HTMLInputElement>(null)
  const selection = useFileSelection()
  const clipboard = useFileClipboard()
  const uploadQueue = useUploadQueue()
  const editorFullScreen = useMediaQuery('(max-width: 48rem)')
  const lastReportedPath = useRef('')

  useEffect(() => {
    setCurrentPath(resolveInitialPath(initialPath, rootPath, allowedRoots, lockRoot))
    selection.clear()
    setEditingFile(null)
    setPreviewingImage(null)
  }, [allowedRoots, initialPath, rootPath, lockRoot])

  useEffect(() => {
    // 将当前目录抛给页面层，由页面层写入 URL；刷新后可恢复到同一目录。
    if (lastReportedPath.current === currentPath) return
    lastReportedPath.current = currentPath
    onPathChange?.(currentPath)
  }, [currentPath, onPathChange])

  const listQuery = useQuery({
    queryKey: ['files', siteId || 'global', currentPath],
    queryFn: () => fileApi.list(currentPath),
    enabled: Boolean(currentPath),
  })

  const entries = useMemo(() => (listQuery.data?.entries || []).slice().sort(sortEntries), [listQuery.data?.entries])
  const entriesByName = useMemo(() => new Map(entries.map((entry) => [entry.name, entry] as const)), [entries])
  const selectedEntries = useMemo(() => selection.selected.map((name) => entriesByName.get(name)).filter((entry): entry is FileEntry => Boolean(entry)), [entriesByName, selection.selected])
  const selectedPaths = selectedEntries.map((entry) => joinPath(currentPath, entry.name))
  const largeDirectory = entries.length > 1000
  const effectiveAllowedRoots = useMemo(() => {
    const roots = (allowedRoots && allowedRoots.length > 0 ? allowedRoots : [rootPath])
      .map(normalizePath)
      .filter(Boolean)
    return Array.from(new Set(roots))
  }, [allowedRoots, rootPath])
  const effectiveLockRoot = normalizePath(lockRoot || '')
  const entryNames = useMemo(() => entries.map((entry) => entry.name), [entries])

  const pathParts = useMemo(() => {
    const parts = currentPath.split('/').filter(Boolean)
    const result: { label: string; path: string; locked: boolean }[] = []
    let accumulated = ''
    for (const part of parts) {
      accumulated += `/${part}`
      result.push({
        label: part,
        path: accumulated,
        locked: Boolean(effectiveLockRoot && !isPathWithinRoot(accumulated, effectiveLockRoot)),
      })
    }
    return result
  }, [currentPath, effectiveLockRoot])

  function navigateTo(path: string): boolean {
    const nextPath = normalizePath(path)
    if (!nextPath) {
      notifyWarning({ message: '请输入有效路径' })
      return false
    }
    if (effectiveAllowedRoots.length > 0 && !effectiveAllowedRoots.some((root) => isPathWithinRoot(nextPath, root))) {
      notifyWarning({ message: '不允许跳转到白名单目录之外' })
      return false
    }
    if (effectiveLockRoot && !isPathWithinRoot(nextPath, effectiveLockRoot)) {
      notifyWarning({ message: '不允许导航到根目录之外' })
      return false
    }
    setCurrentPath(nextPath)
    selection.clear()
    return true
  }

  async function refresh() {
    selection.clear()
    await listQuery.refetch()
  }

  function openPrompt(mode: PromptMode, entry?: FileEntry) {
    if (mode === 'file') setPromptState({ mode, title: '新建文件', label: '文件名', value: '' })
    if (mode === 'dir') setPromptState({ mode, title: '新建目录', label: '目录名', value: '' })
    if (mode === 'rename' && entry) setPromptState({ mode, title: '重命名', label: '新名称', value: entry.name, entry })
  }

  async function submitPrompt() {
    if (!promptState) return
    const value = promptState.value.trim()
    if (!value) {
      notifyWarning({ message: '请输入名称' })
      return
    }
    setPromptSubmitting(true)
    try {
      if (promptState.mode === 'file') await fileApi.writeEmpty(joinPath(currentPath, value))
      if (promptState.mode === 'dir') await fileApi.mkdir(joinPath(currentPath, value))
      if (promptState.mode === 'rename' && promptState.entry && value !== promptState.entry.name) {
        await fileApi.move(joinPath(currentPath, promptState.entry.name), joinPath(currentPath, value))
      }
      notifySuccess({ message: promptState.mode === 'rename' ? '重命名成功' : '创建成功' })
      if (promptState.mode === 'file') setEditingFile({ path: joinPath(currentPath, value), name: value })
      setPromptState(null)
      await refresh()
    } catch (error) {
      showErrorModal(error, promptState.mode === 'rename' ? '重命名失败' : '创建失败')
    } finally {
      setPromptSubmitting(false)
    }
  }

  function deleteEntries(paths: string[], message: string) {
    confirmDanger({
      title: '确认删除',
      message,
      confirmLabel: '删除',
      errorTitle: '删除失败',
      onConfirm: async () => {
        await fileApi.remove(paths)
        notifySuccess({ message: '删除成功' })
        await refresh()
      },
    })
  }

  function downloadPaths(paths: string[], entriesToDownload: FileEntry[]) {
    if (paths.length === 0) {
      notifyWarning({ message: '请先选择文件或目录' })
      return
    }

    // 单个普通文件走 download，目录或多选走 archive，保持旧前端下载语义。
    const only = entriesToDownload[0]
    const url = paths.length === 1 && only && !only.is_dir ? fileApi.downloadUrl(paths[0]) : fileApi.archiveUrl(paths)
    window.open(url, '_blank', 'noopener,noreferrer')
  }

  function openFile(entry: FileEntry) {
    if (entry.is_dir) {
      navigateTo(joinPath(currentPath, entry.name))
      return
    }

    const filePath = joinPath(currentPath, entry.name)
    const category = getFileCategory(entry.name)
    if (category === FileCategory.Text) setEditingFile({ path: filePath, name: entry.name })
    else if (category === FileCategory.Image) setPreviewingImage({ path: filePath, name: entry.name })
    else notifyWarning({ message: '该文件类型暂不支持预览，请下载查看' })
  }

  async function handleUploadChange(event: React.ChangeEvent<HTMLInputElement>) {
    const files = Array.from(event.currentTarget.files || [])
    event.currentTarget.value = ''
    if (files.length === 0) return
    await uploadQueue.startUpload({
      files,
      targetPath: (file) => joinPath(currentPath, file.name),
      upload: fileApi.upload,
      onDone: refresh,
    })
  }

  function handleCopy(mode: 'copy' | 'cut') {
    if (selectedPaths.length === 0) {
      notifyWarning({ message: '请先选择文件或目录' })
      return
    }
    if (mode === 'copy') clipboard.copy(selectedPaths, currentPath)
    else clipboard.cut(selectedPaths, currentPath)
    notifySuccess({ message: mode === 'copy' ? `已复制 ${selectedPaths.length} 个项目到剪贴板` : `已剪切 ${selectedPaths.length} 个项目到剪贴板` })
  }

  async function handlePaste() {
    if (!clipboard.clipboard) return
    const { mode, paths, sourceDir } = clipboard.clipboard
    if (sourceDir === currentPath && mode === 'copy') {
      notifyWarning({ message: '源目录和目标目录相同，请导航到其他目录后粘贴' })
      return
    }

    try {
      if (mode === 'copy') {
        await fileApi.copy(paths, currentPath)
      } else {
        for (const source of paths) {
          const fileName = source.split('/').pop()
          if (fileName) await fileApi.move(source, joinPath(currentPath, fileName))
        }
      }
      clipboard.clearClipboard()
      notifySuccess({ message: mode === 'copy' ? '粘贴完成' : '移动完成' })
      await refresh()
    } catch (error) {
      showErrorModal(error, mode === 'copy' ? '复制失败' : '移动失败')
    }
  }

  function openPermission(entry?: FileEntry) {
    const target = entry || selectedEntries[0]
    if (!target) {
      notifyWarning({ message: '请先选择文件或目录' })
      return
    }
    setPermissionState({
      entry: target,
      path: joinPath(currentPath, target.name),
      mode: target.mode,
      owner: target.owner,
      group: target.group,
      recursive: true,
      submitting: false,
    })
  }

  async function submitPermission() {
    if (!permissionState) return
    setPermissionState({ ...permissionState, submitting: true })
    try {
      await fileApi.chmod(permissionState.path, permissionState.mode, permissionState.recursive)
      await fileApi.chown(permissionState.path, permissionState.owner, permissionState.group, permissionState.recursive)
      notifySuccess({ message: '权限/所有者已更新' })
      setPermissionState(null)
      await refresh()
    } catch (error) {
      showErrorModal(error, '修改权限/所有者失败')
      setPermissionState((current) => current ? { ...current, submitting: false } : current)
    }
  }

  function openCompress() {
    if (selectedEntries.length === 0) {
      notifyWarning({ message: '请先选择文件或目录' })
      return
    }
    const baseName = selectedEntries.length === 1 ? selectedEntries[0].name.replace(/\.[^.]+$/, '') : 'archive'
    setCompressState({ outputName: `${baseName}-${compressTimestamp()}.zip`, format: 'zip', submitting: false })
  }

  async function submitCompress() {
    if (!compressState) return
    const outputName = compressState.outputName.trim()
    if (!outputName) {
      notifyWarning({ message: '请输入文件名' })
      return
    }
    setCompressState({ ...compressState, submitting: true })
    try {
      await fileApi.compress(selectedPaths, joinPath(currentPath, outputName), compressState.format)
      notifySuccess({ message: '压缩完成' })
      setCompressState(null)
      await refresh()
    } catch (error) {
      showErrorModal(error, '压缩失败')
      setCompressState((current) => current ? { ...current, submitting: false } : current)
    }
  }

  function openExtract(entry: FileEntry) {
    const archivePath = joinPath(currentPath, entry.name)
    const baseName = entry.name.replace(/\.(tar\.gz|tgz|zip|tar)$/i, '')
    setExtractState({ archivePath, destDir: joinPath(currentPath, baseName), submitting: false })
  }

  async function submitExtract() {
    if (!extractState) return
    if (!extractState.archivePath || !extractState.destDir) {
      notifyWarning({ message: '参数不完整' })
      return
    }
    setExtractState({ ...extractState, submitting: true })
    try {
      await fileApi.extract(extractState.archivePath, extractState.destDir)
      notifySuccess({ message: '解压完成' })
      setExtractState(null)
      await refresh()
    } catch (error) {
      showErrorModal(error, '解压失败')
      setExtractState((current) => current ? { ...current, submitting: false } : current)
    }
  }

  return (
    <Stack gap="sm" className="fileManagerShell">
      <input ref={uploadInputRef} type="file" multiple hidden onChange={handleUploadChange} />
      <FileToolbar
        selectedCount={selection.selected.length}
        clipboard={clipboard.clipboard}
        loading={listQuery.isFetching}
        onNewFile={() => openPrompt('file')}
        onNewDir={() => openPrompt('dir')}
        onUpload={() => uploadInputRef.current?.click()}
        onDownload={() => downloadPaths(selectedPaths, selectedEntries)}
        onCopy={() => handleCopy('copy')}
        onCut={() => handleCopy('cut')}
        onPaste={handlePaste}
        onPermission={() => openPermission()}
        onCompress={openCompress}
        onClearClipboard={clipboard.clearClipboard}
        onDeleteSelected={() => deleteEntries(selectedPaths, `确定删除选中的 ${selectedPaths.length} 个项目？此操作不可恢复。`)}
        onRefresh={refresh}
      />
      <FilePathBar currentPath={currentPath} parts={pathParts} onNavigate={navigateTo} />
      {largeDirectory ? <Alert color="yellow" icon={<IconAlertTriangle size={16} />} title="大目录保护">当前目录包含 {entries.length} 项，表格保持轻量渲染；如操作卡顿，建议先进入子目录或减少批量选择。</Alert> : null}
      {listQuery.isError ? <ErrorAlert error={listQuery.error} title="加载文件列表失败" /> : null}
      <FileTable
        entries={entries}
        loading={listQuery.isLoading}
        selectedSet={selection.selectedSet}
        onToggle={selection.toggle}
        onToggleAll={(checked) => selection.toggleAll(entryNames, checked)}
        onOpen={openFile}
        onEdit={(entry) => setEditingFile({ path: joinPath(currentPath, entry.name), name: entry.name })}
        onPreview={(entry) => setPreviewingImage({ path: joinPath(currentPath, entry.name), name: entry.name })}
        onDownload={(entry) => downloadPaths([joinPath(currentPath, entry.name)], [entry])}
        onExtract={openExtract}
        onPermission={openPermission}
        onRename={(entry) => openPrompt('rename', entry)}
        onDelete={(entry) => deleteEntries([joinPath(currentPath, entry.name)], `确定删除「${entry.name}」？${entry.is_dir ? '目录内所有内容将被删除，' : ''}此操作不可恢复。`)}
      />

      <Modal opened={Boolean(promptState)} onClose={() => setPromptState(null)} title={promptState?.title} size="sm" closeOnClickOutside={false}>
        <Stack gap="md">
          <TextInput
            label={promptState?.label}
            value={promptState?.value || ''}
            onChange={(event) => { const value = event.currentTarget.value; setPromptState((current) => current ? { ...current, value } : current) }}
            onKeyDown={(event) => { if (event.key === 'Enter') void submitPrompt() }}
            autoFocus
          />
          <Group justify="flex-end">
            <Button variant="default" onClick={() => setPromptState(null)}>取消</Button>
            <Button loading={promptSubmitting} onClick={submitPrompt}>确认</Button>
          </Group>
        </Stack>
      </Modal>

      <UploadProgressModal
        opened={uploadQueue.opened}
        uploading={uploadQueue.uploading}
        items={uploadQueue.items}
        onClose={() => uploadQueue.setOpened(false)}
        onCancel={uploadQueue.cancelAll}
      />

      <FilePermissionModal
        opened={Boolean(permissionState)}
        entry={permissionState?.entry || null}
        path={permissionState?.path || ''}
        mode={permissionState?.mode || ''}
        owner={permissionState?.owner || ''}
        group={permissionState?.group || ''}
        recursive={Boolean(permissionState?.recursive)}
        submitting={permissionState?.submitting}
        onChange={(patch) => setPermissionState((current) => current ? { ...current, ...patch } : current)}
        onClose={() => setPermissionState(null)}
        onSubmit={submitPermission}
      />

      <FileCompressModal
        opened={Boolean(compressState)}
        selectedNames={selectedEntries.map((entry) => entry.name)}
        outputName={compressState?.outputName || ''}
        format={compressState?.format || 'zip'}
        submitting={compressState?.submitting}
        onChange={(patch) => setCompressState((current) => {
          if (!current) return current
          const next = { ...current, ...patch }
          // 切换压缩格式时同步替换默认后缀，避免格式和文件名不一致。
          if (patch.format && patch.format !== current.format) {
            const oldExt = patch.format === 'tar.gz' ? '.zip' : '.tar.gz'
            const newExt = compressDefaultExt(patch.format)
            if (next.outputName.endsWith(oldExt)) next.outputName = next.outputName.slice(0, -oldExt.length) + newExt
          }
          return next
        })}
        onClose={() => setCompressState(null)}
        onSubmit={submitCompress}
      />

      <FileExtractModal
        opened={Boolean(extractState)}
        archivePath={extractState?.archivePath || ''}
        destDir={extractState?.destDir || ''}
        submitting={extractState?.submitting}
        onDestDirChange={(destDir) => setExtractState((current) => current ? { ...current, destDir } : current)}
        onClose={() => setExtractState(null)}
        onSubmit={submitExtract}
      />

      <Modal opened={Boolean(editingFile)} onClose={() => setEditingFile(null)} title={editingFile?.name || '编辑文件'} size="90vw" fullScreen={editorFullScreen} closeOnClickOutside={false}>
        {editingFile ? <FileEditorPanel fileApi={fileApi} filePath={editingFile.path} fileName={editingFile.name} onClose={() => setEditingFile(null)} onSaved={refresh} /> : null}
      </Modal>

      <ImagePreviewModal
        opened={Boolean(previewingImage)}
        title={previewingImage?.name || '图片预览'}
        path={previewingImage?.path || ''}
        url={previewingImage ? fileApi.downloadUrl(previewingImage.path) : ''}
        onClose={() => setPreviewingImage(null)}
      />
    </Stack>
  )
}
