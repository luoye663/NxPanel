import { Button } from '@mantine/core'
import { modals } from '@mantine/modals'
import { showErrorModal } from '@/utils/errorModal'

interface ConfirmDangerOptions {
  title: string
  message: string
  confirmLabel?: string
  cancelLabel?: string
  errorTitle?: string
  onConfirm: () => void | Promise<void>
}

export function confirmDanger({
  title,
  message,
  confirmLabel = '确认',
  cancelLabel = '取消',
  errorTitle,
  onConfirm,
}: ConfirmDangerOptions) {
  // 危险动作统一二次确认，避免删除、清空、覆盖等操作散落在页面中各自实现。
  modals.openConfirmModal({
    title,
    children: message,
    labels: { confirm: confirmLabel, cancel: cancelLabel },
    confirmProps: { color: 'red' },
    cancelProps: { variant: 'default' },
    closeOnConfirm: false,
    onConfirm: async () => {
      modals.closeAll()
      try {
        await onConfirm()
      } catch (error) {
        showErrorModal(error, errorTitle || `${title}失败`)
      }
    },
  })
}

interface ConfirmActionProps {
  label: string
  onClick: () => void | Promise<void>
}

export function DangerConfirmButton({ label, onClick }: ConfirmActionProps) {
  return (
    <Button color="red" onClick={onClick}>
      {label}
    </Button>
  )
}
