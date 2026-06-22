import { Alert, Badge, Button, Group, Loader, Stack, Text } from '@mantine/core'
import { modals } from '@mantine/modals'
import { IconAlertTriangle } from '@tabler/icons-react'
import { lazy, Suspense, useEffect, useState } from 'react'
import type { FileApiBridge } from '@/hooks/useFileApi'
import { getCodeEditorLanguage } from '@/utils/fileType'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

const NginxCodeEditor = lazy(() => import('@/components/editor/NginxCodeEditor'))

interface FileEditorPanelProps {
  fileApi: FileApiBridge
  filePath: string
  fileName: string
  onClose: () => void
  onSaved: () => void
}

function base64ToText(contentBase64: string): string {
  const binary = atob(contentBase64)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i)
  return new TextDecoder('utf-8', { fatal: false }).decode(bytes)
}

function textToBase64(value: string): string {
  const bytes = new TextEncoder().encode(value)
  let binary = ''
  // 大文件不能一次性 String.fromCharCode(...bytes)，分片避免调用栈溢出。
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.slice(i, i + 0x8000))
  }
  return btoa(binary)
}

export function FileEditorPanel({ fileApi, filePath, fileName, onClose, onSaved }: FileEditorPanelProps) {
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [content, setContent] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [dirty, setDirty] = useState(false)
  const editorLanguage = getCodeEditorLanguage(fileName || filePath)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    setDirty(false)
    fileApi.read(filePath)
      .then((result) => {
        if (!cancelled) setContent(base64ToText(result.content_base64))
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : '读取文件失败')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    // 文件切换或弹窗卸载时忽略旧请求结果，避免把上一个文件内容写回当前状态。
    return () => { cancelled = true }
  }, [fileApi, filePath])

  async function saveContent(): Promise<boolean> {
    if (!dirty) return true
    setSaving(true)
    try {
      await fileApi.write(filePath, textToBase64(content))
      setDirty(false)
      notifySuccess({ message: '文件已保存' })
      onSaved()
      return true
    } catch (err) {
      showErrorModal(err, '保存文件失败')
      return false
    } finally {
      setSaving(false)
    }
  }

  function requestClose() {
    if (!dirty) {
      onClose()
      return
    }

    modals.openConfirmModal({
      title: '未保存的更改',
      children: '文件已修改，是否保存后关闭？',
      labels: { confirm: '保存', cancel: '不保存' },
      closeOnConfirm: false,
      onCancel: onClose,
      onConfirm: async () => {
        modals.closeAll()
        const saved = await saveContent()
        if (saved) onClose()
      },
    })
  }

  return (
    <Stack gap="sm" className="fileEditorPanel">
      <Group justify="space-between" align="flex-start" gap="sm" className="fileEditorHeader">
        <Group gap="xs" className="fileEditorTitle">
          <Badge color="dark">FILE</Badge>
          <Text fw={600}>{fileName}</Text>
          <Text ff="monospace" size="xs" c="dimmed" className="monoEllipsis">{filePath}</Text>
          {dirty ? <Badge color="yellow" variant="light">未保存</Badge> : null}
        </Group>
        <Group gap="xs">
          <Button loading={saving} disabled={!dirty} onClick={saveContent}>保存</Button>
          <Button variant="default" onClick={requestClose}>关闭</Button>
        </Group>
      </Group>

      {loading ? <Group justify="center" py="xl"><Loader size="sm" /><Text size="sm" c="dimmed">正在读取文件...</Text></Group> : null}
      {!loading && error ? <Alert color="red" icon={<IconAlertTriangle size={16} />} title="读取文件失败">{error}</Alert> : null}
      {!loading && !error ? (
        <div className="nginxEditorShell fileEditorCodeShell">
          <Suspense fallback={<Group justify="center" py="xl"><Loader size="sm" /><Text size="sm" c="dimmed">正在加载编辑器...</Text></Group>}>
            <NginxCodeEditor language={editorLanguage} value={content} onChange={(value) => { setContent(value); setDirty(true) }} />
          </Suspense>
        </div>
      ) : null}
    </Stack>
  )
}
