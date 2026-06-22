import { Alert, Button, Group, Loader, Stack, Text } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconDeviceFloppy, IconRefresh } from '@tabler/icons-react'
import { lazy, Suspense, useEffect, useState } from 'react'
import { ApiError } from '@/api/client'
import { getSiteConfig, saveSiteConfig } from '@/api/config'
import type { SiteDetail } from '@/api/types'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { PathCell } from '@/components/common/PathCell'
import { SectionCard } from '@/components/common/SectionCard'
import { siteDetailKeys } from '@/hooks/useSiteDetail'
import { confirmDanger } from '@/utils/confirm'
import { notifySuccess, notifyWarning } from '@/utils/notify'

interface SiteConfigTabProps {
  site: SiteDetail
}

const NginxCodeEditor = lazy(() => import('@/components/editor/NginxCodeEditor'))

export function SiteConfigTab({ site }: SiteConfigTabProps) {
  const queryClient = useQueryClient()
  const configQueryKey = ['site-detail', site.id, 'config'] as const
  const configQuery = useQuery({ queryKey: configQueryKey, queryFn: () => getSiteConfig(site.id) })
  const saveMutation = useMutation({ mutationFn: (data: { content: string; expected_hash?: string; danger_confirmed: boolean }) => saveSiteConfig(site.id, data) })
  const [content, setContent] = useState('')
  const [hash, setHash] = useState('')
  const [syncWarnings, setSyncWarnings] = useState<string[]>([])

  useEffect(() => {
    if (!configQuery.data) return
    setContent(configQuery.data.content)
    setHash(configQuery.data.hash)
    setSyncWarnings([])
  }, [configQuery.data])

  async function reloadConfig() {
    setSyncWarnings([])
    const result = await configQuery.refetch()
    if (result.data) {
      setContent(result.data.content)
      setHash(result.data.hash)
    }
  }

  function handleSave() {
    confirmDanger({
      title: '保存完整配置',
      message: '完整配置编辑会直接覆盖站点 conf 文件。保存后将执行 nginx -t 并 reload Nginx，请确认已保留 NxPanel 标识块。',
      confirmLabel: '确认保存',
      errorTitle: '保存完整配置失败',
      onConfirm: async () => {
        setSyncWarnings([])
        try {
          const result = await saveMutation.mutateAsync({ content, expected_hash: hash || undefined, danger_confirmed: true })
          setHash(result.hash)
          setSyncWarnings(result.sync_warnings || [])
          notifySuccess({ message: '完整配置已保存' })
          await Promise.all([
            queryClient.invalidateQueries({ queryKey: configQueryKey }),
            queryClient.invalidateQueries({ queryKey: siteDetailKeys.detail(site.id) }),
            queryClient.invalidateQueries({ queryKey: ['sites'] }),
          ])
        } catch (error) {
          if (error instanceof ApiError && error.code === 'CONFIG_DRIFTED') {
            // 发现配置漂移时先重新加载，避免把旧 hash 对应的内容覆盖到用户新修改的文件。
            notifyWarning({ message: '站点配置已被外部修改，正在重新加载...' })
            await reloadConfig()
            return
          }
          throw error
        }
      },
    })
  }

  return (
    <SectionCard className="siteEditorCard">
      <Stack gap="md" className="siteEditorStack">
        {syncWarnings.length ? <Alert color="yellow" title="同步警告">{syncWarnings.map((warning) => <div key={warning}>{warning}</div>)}</Alert> : null}

        <Group justify="space-between" gap="sm">
          <Group gap="xs">
            <Button variant="light" leftSection={<IconRefresh size={16} />} loading={configQuery.isFetching} onClick={reloadConfig}>重新加载</Button>
            <Button leftSection={<IconDeviceFloppy size={16} />} loading={saveMutation.isPending} disabled={configQuery.isLoading} onClick={handleSave}>保存配置</Button>
          </Group>
          {site.config_path ? <PathCell value={site.config_path} maxWidth={320} justify="flex-end" /> : null}
        </Group>
        {configQuery.isError ? <ErrorAlert error={configQuery.error} title="加载完整配置失败" /> : null}
        <div className="nginxEditorShell siteConfigEditorShell">
          {configQuery.isLoading ? (
            <Group h="100%" justify="center"><Loader size="sm" /><Text size="sm" c="dimmed">正在加载站点配置...</Text></Group>
          ) : configQuery.isError ? (
            <Group h="100%" justify="center"><Text size="sm" c="dimmed">配置文件当前不可读取，请先处理上方错误提示。</Text></Group>
          ) : (
            <Suspense fallback={<Group h="100%" justify="center"><Loader size="sm" /><Text size="sm" c="dimmed">正在加载编辑器...</Text></Group>}>
              {/* 完整配置编辑器按 tab 懒加载，避免站点详情首屏加载 CodeMirror。 */}
              <NginxCodeEditor value={content} onChange={setContent} formattable />
            </Suspense>
          )}
        </div>
      </Stack>
    </SectionCard>
  )
}
