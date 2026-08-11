import { Modal } from '@mantine/core'
import { useMediaQuery } from '@mantine/hooks'
import { UpstreamHelpLabel } from './UpstreamHelpLabel'
import { UpstreamManagerContent } from './NginxUpstreamsTab'

interface UpstreamManagerModalProps {
  opened: boolean
  onClose: () => void
}

export default function UpstreamManagerModal({ opened, onClose }: UpstreamManagerModalProps) {
  const mobile = useMediaQuery('(max-width: 48rem)')

  return (
    <Modal
      opened={opened}
      onClose={onClose}
      title={<UpstreamHelpLabel label="上游组管理" help="在当前页面创建、编辑、删除和同步全局上游组。变更会立即反映到反向代理的上游组选择器。" />}
      size="xl"
      fullScreen={mobile}
      closeOnClickOutside={false}
      centered
    >
      <UpstreamManagerContent active={opened} embedded />
    </Modal>
  )
}
