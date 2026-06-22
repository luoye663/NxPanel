import { Button, Group, Loader, Stack, Text } from '@mantine/core'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { IconDeviceFloppy, IconRefresh } from '@tabler/icons-react'
import { lazy, Suspense, useEffect, useState } from 'react'
import { ApiError } from '@/api/client'
import { saveNginxConf } from '@/api/nginx'
import { queryKeys, useNginxConf } from '@/api/hooks'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { SectionCard } from '@/components/common/SectionCard'
import { confirmDanger } from '@/utils/confirm'
import { notifySuccess, notifyWarning } from '@/utils/notify'

const NginxCodeEditor = lazy(() => import('@/components/editor/NginxCodeEditor'))

export function NginxConfEditorTab({ active }: { active: boolean }) {
  const queryClient = useQueryClient()
  const confQuery = useNginxConf(active)
  const saveMutation = useMutation({ mutationFn: saveNginxConf })
  const [content, setContent] = useState('')
  const [hash, setHash] = useState('')

  useEffect(() => {
    if (!confQuery.data) return
    setContent(confQuery.data.content)
    setHash(confQuery.data.hash)
  }, [confQuery.data])

  async function reloadConf() {
    const result = await confQuery.refetch()
    if (result.data) {
      setContent(result.data.content)
      setHash(result.data.hash)
    }
  }

  function handleSave() {
    confirmDanger({
      title: '保存 nginx.conf',
      message: '直接编辑 nginx.conf 是危险操作。保存后将执行 nginx -t 验证并 reload Nginx。确认保存？',
      confirmLabel: '确认保存',
      errorTitle: '保存 nginx.conf 失败',
      onConfirm: async () => {
        try {
          const result = await saveMutation.mutateAsync({ content, expected_hash: hash, danger_confirmed: true })
          setHash(result.hash)
          notifySuccess({ message: 'nginx.conf 保存成功，Nginx 已 reload' })
          await queryClient.invalidateQueries({ queryKey: queryKeys.systemOverview })
          await queryClient.invalidateQueries({ queryKey: queryKeys.nginxConf })
        } catch (error) {
          if (error instanceof ApiError && error.code === 'CONFIG_DRIFTED') {
            // 后端发现配置漂移时先重新加载，避免用旧 hash 覆盖用户或其他进程的修改。
            notifyWarning({ message: '配置文件已被外部修改，正在重新加载...' })
            await reloadConf()
            return
          }
          throw error
        }
      },
    })
  }

  return (
    <Stack gap="sm" className="nginxConfEditorTab">
      <SectionCard className="nginxConfEditorCard">
        <Group mb="sm">
          <Button variant="light" leftSection={<IconRefresh size={16} />} loading={confQuery.isFetching} onClick={reloadConf}>加载配置</Button>
          <Button leftSection={<IconDeviceFloppy size={16} />} loading={saveMutation.isPending} disabled={!content} onClick={handleSave}>保存并 Reload</Button>
          {hash ? <Text size="xs" c="dimmed" ff="monospace" style={{ overflowWrap: 'anywhere' }}>Hash: {hash}</Text> : null}
        </Group>
        {confQuery.isError ? <ErrorAlert error={confQuery.error} title="加载 nginx.conf 失败" /> : null}
        <div className="nginxEditorShell nginxConfEditorShell">
          {content ? (
            <Suspense fallback={<Group h="100%" justify="center"><Loader size="sm" /><Text size="sm" c="dimmed">正在加载编辑器...</Text></Group>}>
              <NginxCodeEditor value={content} onChange={setContent} formattable />
            </Suspense>
          ) : <Group h="100%" justify="center"><Text c="dimmed">点击「加载配置」读取 nginx.conf 内容</Text></Group>}
        </div>
      </SectionCard>
    </Stack>
  )
}
