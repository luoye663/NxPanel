import { Alert, Stack } from '@mantine/core'
import type { SiteDetail } from '@/api/types'
import { SectionCard } from '@/components/common/SectionCard'
import { FileManager } from '@/components/files/FileManager'

interface SiteFilesTabProps {
  site: SiteDetail
}

export function SiteFilesTab({ site }: SiteFilesTabProps) {
  return (
    <SectionCard>
      <Stack gap="md">
        <Alert color="blue">
          站点文件管理只允许在当前网站根目录内导航和操作，避免误进入其他站点或系统目录。
        </Alert>
        {/* 站点级 FileManager 复用全局文件管理器，通过 siteId 自动切换到 /sites/{id}/files API。 */}
        <FileManager siteId={site.id} rootPath={site.root_path} lockRoot={site.root_path} />
      </Stack>
    </SectionCard>
  )
}
