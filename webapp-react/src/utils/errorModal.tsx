import { modals } from '@mantine/modals'
import { ErrorAlert } from '@/components/common/ErrorAlert'

export function showErrorModal(error: unknown, title = '操作失败') {
  modals.open({
    title,
    size: 'lg',
    centered: true,
    children: <ErrorAlert error={error} title={title} />,
  })
}
