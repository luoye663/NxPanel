import { notifications } from '@mantine/notifications'

type NotifyOptions = {
  title?: string
  message: string
}

export function notifySuccess({ title = '操作成功', message }: NotifyOptions) {
  notifications.show({ title, message, color: 'green' })
}

export function notifyError({ title = '操作失败', message }: NotifyOptions) {
  // 通知只展示摘要，避免把 token、私钥、secret 等敏感 details 带到 UI。
  notifications.show({ title, message, color: 'red' })
}

export function notifyWarning({ title = '请注意', message }: NotifyOptions) {
  notifications.show({ title, message, color: 'yellow' })
}
