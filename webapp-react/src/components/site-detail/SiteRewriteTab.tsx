import { Button, Group, Loader, Stack, Text } from '@mantine/core'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconDeviceFloppy, IconRefresh, IconTemplate } from '@tabler/icons-react'
import { lazy, Suspense, useEffect, useState } from 'react'
import { ApiError } from '@/api/client'
import { getRewrite, updateRewrite, type UpdateRewriteRequest } from '@/api/rewrite'
import type { SiteDetail } from '@/api/types'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { PathCell } from '@/components/common/PathCell'
import { SectionCard } from '@/components/common/SectionCard'
import { siteDetailKeys } from '@/hooks/useSiteDetail'
import { confirmDanger } from '@/utils/confirm'
import { notifySuccess, notifyWarning } from '@/utils/notify'

interface SiteRewriteTabProps {
  site: SiteDetail
}

const NginxCodeEditor = lazy(() => import('@/components/editor/NginxCodeEditor'))
const RewriteTemplateModal = lazy(() => import('./RewriteTemplateModal'))

export function SiteRewriteTab({ site }: SiteRewriteTabProps) {
  const queryClient = useQueryClient()
  const rewriteQueryKey = ['site-detail', site.id, 'rewrite'] as const
  const rewriteQuery = useQuery({ queryKey: rewriteQueryKey, queryFn: () => getRewrite(site.id) })
  const saveMutation = useMutation({ mutationFn: (data: UpdateRewriteRequest) => updateRewrite(site.id, data) })
  const [content, setContent] = useState('')
  const [hash, setHash] = useState('')
  const [templateOpened, setTemplateOpened] = useState(false)

  useEffect(() => {
    if (!rewriteQuery.data) return
    setContent(rewriteQuery.data.content)
    setHash(rewriteQuery.data.content_hash)
  }, [rewriteQuery.data])

  async function reloadRewrite() {
    const result = await rewriteQuery.refetch()
    if (result.data) {
      setContent(result.data.content)
      setHash(result.data.content_hash)
    }
  }

  function handleSave() {
    confirmDanger({
      title: '保存自定义 Location',
      message: '自定义 Location 片段可破坏站点配置。保存后将执行 nginx -t 测试并 reload Nginx，确认保存？',
      confirmLabel: '确认保存',
      errorTitle: '保存自定义 Location 失败',
      onConfirm: async () => {
        try {
          const result = await saveMutation.mutateAsync({ content, expected_content_hash: hash || undefined, danger_confirmed: true })
          setHash(result.content_hash)
          notifySuccess({ message: '自定义 Location 已保存' })
          await Promise.all([
            queryClient.invalidateQueries({ queryKey: rewriteQueryKey }),
            queryClient.invalidateQueries({ queryKey: siteDetailKeys.detail(site.id) }),
          ])
        } catch (error) {
          if (error instanceof ApiError && error.code === 'CONFIG_DRIFTED') {
            // 内容 hash 冲突时先重新加载，避免覆盖用户在其他窗口或进程中的修改。
            notifyWarning({ message: '自定义 Location 文件已被外部修改，正在重新加载...' })
            await reloadRewrite()
            return
          }
          throw error
        }
      },
    })
  }

  function appendTemplate(templateContent: string) {
    setContent((current) => `${current.trimEnd()}\n\n${templateContent}`.trimStart())
  }

  function handleTemplateInsert(templateContent: string) {
    if (!content.trim()) {
      setContent(templateContent)
      notifySuccess({ message: '模板已插入编辑框' })
      return
    }

    modals.open({
      title: '插入 Location 模板',
      children: (
        <Stack gap="md">
          <Text size="sm">当前 Location 编辑框已有内容，是否覆盖当前内容？选择不覆盖会把模板追加到最后一行。</Text>
          <Group justify="flex-end">
            <Button variant="default" onClick={() => {
              appendTemplate(templateContent)
              modals.closeAll()
              notifySuccess({ message: '模板已追加到编辑框末尾' })
            }}>不覆盖，追加到最后</Button>
            <Button color="red" onClick={() => {
              setContent(templateContent)
              modals.closeAll()
              notifySuccess({ message: '模板已覆盖当前编辑框内容' })
            }}>覆盖当前内容</Button>
          </Group>
        </Stack>
      ),
    })
  }

  return (
    <SectionCard className="siteEditorCard">
      <Stack gap="md" className="siteEditorStack">
        {/* <Alert color="yellow" title="保存前请确认语法">
          Location 用于按访问路径配置站点规则，例如 /api 反向代理、/uploads 静态目录、/admin 访问限制等。
        </Alert> */}
        <Group justify="space-between" align="center" gap="sm">
          <Group gap="xs">
            <Button variant="light" leftSection={<IconRefresh size={16} />} loading={rewriteQuery.isFetching} onClick={reloadRewrite}>重新加载</Button>
            <Button variant="light" leftSection={<IconTemplate size={16} />} onClick={() => setTemplateOpened(true)}>应用模板</Button>
            <Button leftSection={<IconDeviceFloppy size={16} />} loading={saveMutation.isPending} disabled={rewriteQuery.isLoading} onClick={handleSave}>保存 Location</Button>
          </Group>
          {rewriteQuery.data?.path ? <PathCell value={rewriteQuery.data.path} maxWidth={320} justify="flex-end" /> : null}
        </Group>
        {rewriteQuery.isError ? <ErrorAlert error={rewriteQuery.error} title="加载自定义 Location 失败" /> : null}
        <div className="nginxEditorShell siteRewriteEditorShell">
          {rewriteQuery.isLoading ? (
            <Group h="100%" justify="center"><Loader size="sm" /><Text size="sm" c="dimmed">正在加载自定义 Location...</Text></Group>
          ) : (
            <Suspense fallback={<Group h="100%" justify="center"><Loader size="sm" /><Text size="sm" c="dimmed">正在加载编辑器...</Text></Group>}>
              {/* CodeMirror 按 tab 懒加载，避免站点详情首屏一次性加载编辑器体积。 */}
              <NginxCodeEditor value={content} onChange={setContent} formattable />
            </Suspense>
          )}
        </div>
      </Stack>
      <Suspense fallback={null}>
        <RewriteTemplateModal
          opened={templateOpened}
          onClose={() => setTemplateOpened(false)}
          onInsert={handleTemplateInsert}
        />
      </Suspense>
    </SectionCard>
  )
}
