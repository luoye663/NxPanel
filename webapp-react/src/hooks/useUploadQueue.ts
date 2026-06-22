import { useEffect, useRef, useState } from 'react'

export type UploadQueueStatus = 'uploading' | 'done' | 'error' | 'cancelled'

export interface UploadQueueItem {
  id: number
  name: string
  percent: number
  status: UploadQueueStatus
}

interface StartUploadOptions {
  files: File[]
  targetPath: (file: File) => string
  upload: (targetPath: string, file: File, onProgress: (percent: number) => void, signal: AbortSignal) => Promise<unknown>
  onDone?: () => void | Promise<void>
}

export function useUploadQueue() {
  const [items, setItems] = useState<UploadQueueItem[]>([])
  const [opened, setOpened] = useState(false)
  const controllersRef = useRef(new Map<number, AbortController>())

  function updateItem(id: number, patch: Partial<UploadQueueItem>) {
    setItems((current) => current.map((item) => item.id === id ? { ...item, ...patch } : item))
  }

  async function startUpload({ files, targetPath, upload, onDone }: StartUploadOptions) {
    const nextItems = files.map((file, index) => ({ id: Date.now() + index, name: file.name, percent: 0, status: 'uploading' as const }))
    setItems(nextItems)
    setOpened(true)
    controllersRef.current.clear()

    for (let index = 0; index < files.length; index += 1) {
      const file = files[index]
      const item = nextItems[index]
      const controller = new AbortController()
      controllersRef.current.set(item.id, controller)

      try {
        await upload(targetPath(file), file, (percent) => updateItem(item.id, { percent }), controller.signal)
        updateItem(item.id, { percent: 100, status: 'done' })
      } catch {
        updateItem(item.id, { status: controller.signal.aborted ? 'cancelled' : 'error' })
      } finally {
        controllersRef.current.delete(item.id)
      }
    }

    await onDone?.()
  }

  function cancelAll() {
    controllersRef.current.forEach((controller) => controller.abort())
    controllersRef.current.clear()
    setItems((current) => current.map((item) => item.status === 'uploading' ? { ...item, status: 'cancelled' } : item))
  }

  useEffect(() => {
    // 页面卸载时主动 abort，避免上传请求在组件销毁后继续回调 setState。
    return () => cancelAll()
  }, [])

  return { items, opened, setOpened, startUpload, cancelAll, uploading: items.some((item) => item.status === 'uploading') }
}
