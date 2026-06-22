import { ActionIcon, Alert, Badge, Button, Group, ScrollArea, Select, Stack, Text, Tooltip } from '@mantine/core'
import { useMediaQuery } from '@mantine/hooks'
import { IconCode, IconCopy, IconExternalLink, IconFileDescription, IconFileText, IconFolder, IconKey, IconListDetails, IconLock, IconNotebook, IconRoute, IconSettings, IconShield } from '@tabler/icons-react'
import type { ReactNode } from 'react'
import type { SiteDetail } from '@/api/types'
import { StatusBadge } from '@/components/common/StatusBadge'
import { notifyError, notifySuccess } from '@/utils/notify'

export type SiteDetailTab = 'basic' | 'document' | 'proxy' | 'access-limit' | 'hotlink' | 'ssl' | 'rewrite' | 'config' | 'files' | 'logs' | 'operations'

interface SiteTabItem {
  value: SiteDetailTab
  label: string
  description: string
  icon: ReactNode
  importedDisabled?: boolean
}

const tabItems: SiteTabItem[] = [
  { value: 'basic', label: '基础配置', description: '修改域名绑定、站点根目录、默认首页、HTTPS 端口和访问日志开关。', icon: <IconSettings size={16} />, importedDisabled: true },
  { value: 'document', label: '默认文档', description: '配置默认首页顺序、目录浏览 autoindex，以及站点内 403/404 错误页 URI。', icon: <IconFileDescription size={16} />, importedDisabled: true },
  { value: 'proxy', label: '反向代理', description: '为当前站点配置一个或多个 location 反代规则，保存后会由后端测试并 reload Nginx。', icon: <IconRoute size={16} />, importedDisabled: true },
  { value: 'access-limit', label: '访问限制', description: '管理加密访问和禁止访问规则，规则保存后由后端更新独立 include 文件。', icon: <IconKey size={16} />, importedDisabled: true },
  { value: 'hotlink', label: '防盗链', description: '管理站点级 Referer 防盗链规则，保存后会重写独立 hotlink include 文件。', icon: <IconShield size={16} />, importedDisabled: true },
  { value: 'ssl', label: 'SSL', description: '部署手动 PEM、使用已有证书文件或从证书夹部署证书。', icon: <IconLock size={16} />, importedDisabled: true },
  { value: 'rewrite', label: 'Location', description: 'Location 用于按访问路径配置站点规则，例如 /api 反向代理、/uploads 静态目录、/admin 访问限制等。', icon: <IconFileText size={16} />, importedDisabled: true },
  { value: 'config', label: '站点配置', description: '直接编辑当前站点 Nginx 配置文件，需要保留哨兵标记块，否则基础配置、SSL、反向代理等表单功能将无法安全修改对应片段。', icon: <IconCode size={16} /> },
  { value: 'files', label: '文件管理', description: '管理当前站点根目录下的文件，文件浏览器会锁定在站点根目录内。', icon: <IconFolder size={16} /> },
  { value: 'logs', label: '访问日志', description: '查看站点 access/error 日志，可按行数拉取并清空当前日志文件。', icon: <IconNotebook size={16} /> },
  { value: 'operations', label: '操作记录', description: '仅展示当前站点相关的写操作、Nginx 测试和回滚结果。', icon: <IconListDetails size={16} /> },
]

interface SiteDetailShellProps {
  site: SiteDetail
  activeTab: SiteDetailTab
  onTabChange: (tab: SiteDetailTab) => void
  children: ReactNode
  actions?: ReactNode
}

export function isImportedDisabledTab(site: SiteDetail | null | undefined, tab: SiteDetailTab): boolean {
  return Boolean(site?.is_imported && tabItems.some((item) => item.value === tab && item.importedDisabled))
}

export function fallbackSiteDetailTab(site: SiteDetail | null | undefined, tab: SiteDetailTab): SiteDetailTab {
  return isImportedDisabledTab(site, tab) ? 'config' : tab
}

export function getSiteDetailTabMeta(tab: SiteDetailTab): Pick<SiteTabItem, 'label' | 'description'> {
  const item = tabItems.find((entry) => entry.value === tab) || tabItems[0]
  return { label: item.label, description: item.description }
}

export function SiteDetailShell({ site, activeTab, onTabChange, children, actions }: SiteDetailShellProps) {
  const mobile = useMediaQuery('(max-width: 48rem)')
  const primaryDomain = site.primary_domain || ''
  const activeMeta = getSiteDetailTabMeta(activeTab)
  const selectData = tabItems.map((item) => ({
    value: item.value,
    label: item.importedDisabled && site.is_imported ? `${item.label}（旧站点不可用）` : item.label,
    disabled: item.importedDisabled && site.is_imported,
  }))

  function openPrimaryDomain() {
    if (!primaryDomain) return
    // 后端只返回域名时补 HTTP 协议，避免 window.open 把域名当成相对路径。
    const url = /^https?:\/\//i.test(primaryDomain) ? primaryDomain : `http://${primaryDomain}`
    window.open(url, '_blank', 'noopener,noreferrer')
  }

  async function copyPrimaryDomain() {
    if (!primaryDomain) return
    try {
      await navigator.clipboard.writeText(primaryDomain)
      notifySuccess({ message: '域名已复制' })
    } catch {
      notifyError({ message: '复制域名失败，请手动复制' })
    }
  }

  function selectTab(tab: SiteDetailTab) {
    onTabChange(tab)
  }

  return (
    <Stack gap="md" className="siteDetailShell">
      <Group justify="space-between" align="flex-start" gap="sm">
        <Stack gap={4}>
          <Group gap="xs">
            <Text fw={700} size="lg">{site.primary_domain || '网站详情'}</Text>
            {primaryDomain ? (
              <Group gap={4} wrap="nowrap">
                <Tooltip label="新标签打开域名">
                  <ActionIcon variant="subtle" aria-label="新标签打开域名" onClick={openPrimaryDomain}>
                    <IconExternalLink size={16} />
                  </ActionIcon>
                </Tooltip>
                <Tooltip label="复制域名">
                  <ActionIcon variant="subtle" aria-label="复制域名" onClick={copyPrimaryDomain}>
                    <IconCopy size={16} />
                  </ActionIcon>
                </Tooltip>
              </Group>
            ) : null}
            <StatusBadge kind="site" value={site.status} />
            {site.is_imported ? <Badge color="yellow" variant="light">外部导入</Badge> : null}
            {site.is_imported ? <Text size="xs" c="dimmed">注意: 此站点为外部导入，支持直接编辑配置文件。部分表单功能不可用，避免破坏未知配置结构。</Text> : null}
          </Group>
          <Text size="sm" c="dimmed">{activeMeta.description}</Text>
        </Stack>
        {actions}
      </Group>

      {mobile ? (
        <Stack gap="md">
          <Select label="详情功能" data={selectData} value={activeTab} allowDeselect={false} onChange={(value) => value && selectTab(value as SiteDetailTab)} />
          {children}
        </Stack>
      ) : (
        <div className="siteDetailLayout">
          <ScrollArea className="siteDetailNav">
            <Stack gap={6}>
              {tabItems.map((item) => {
                const disabled = Boolean(item.importedDisabled && site.is_imported)
                return (
                  <Button
                    key={item.value}
                    className="siteDetailNavButton"
                    justify="flex-start"
                    leftSection={item.icon}
                    variant={activeTab === item.value ? 'light' : 'subtle'}
                    color={disabled ? 'gray' : 'blue'}
                    disabled={disabled}
                    onClick={() => selectTab(item.value)}
                  >
                    {item.label}
                  </Button>
                )
              })}
            </Stack>
          </ScrollArea>
          <div className={`siteDetailContent ${activeTab === 'rewrite' || activeTab === 'config' ? 'siteDetailContentEditor' : ''} ${activeTab === 'logs' ? 'siteDetailContentLogs' : ''}`}>{children}</div>
        </div>
      )}
    </Stack>
  )
}

export function SiteDetailPlaceholder({ title, description }: { title: string; description: string }) {
  return (
    <Alert color="gray" title={title}>
      {description}
    </Alert>
  )
}
