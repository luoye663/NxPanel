import { Button, Group, Modal, Select, Stack, Switch, Text, TextInput, Textarea } from '@mantine/core'
import { useMutation, useQuery } from '@tanstack/react-query'
import { IconSettings } from '@tabler/icons-react'
import { lazy, Suspense, useEffect, useState } from 'react'
import { getRewriteTemplates, previewRewriteTemplate } from '@/api/rewrite'
import type { RewriteTemplateParam } from '@/api/rewrite'
import { ErrorAlert } from '@/components/common/ErrorAlert'

const RewriteTemplateManager = lazy(() => import('./RewriteTemplateManager'))

interface RewriteTemplateModalProps {
  opened: boolean
  onClose: () => void
  onInsert: (content: string) => void
}

const emptyTemplateParams: RewriteTemplateParam[] = []

export function RewriteTemplateModal({ opened, onClose, onInsert }: RewriteTemplateModalProps) {
  const templatesQuery = useQuery({ queryKey: ['rewrite-templates'], queryFn: getRewriteTemplates, enabled: opened })
  const [templateId, setTemplateId] = useState('')
  const [params, setParams] = useState<Record<string, unknown>>({})
  const [managerOpened, setManagerOpened] = useState(false)
  const previewMutation = useMutation({ mutationFn: () => previewRewriteTemplate({ template_id: templateId, params }) })
  // 应用列表只展示启用模板；管理页维护全部模板
  const templates = (templatesQuery.data?.templates || []).filter((item) => item.enabled)
  const selected = templates.find((item) => item.id === templateId)
  const selectedParams = Array.isArray(selected?.params) ? selected.params : emptyTemplateParams
  const preview = previewMutation.data?.content || ''

  useEffect(() => {
    if (!opened || templateId || templates.length === 0) return
    setTemplateId(templates[0].id)
  }, [opened, templateId, templates])

  useEffect(() => {
    if (!selected) return
    const next: Record<string, unknown> = {}
    for (const param of selectedParams) next[param.key] = param.default
    setParams(next)
  }, [selected, selectedParams])

  useEffect(() => {
    if (!templateId) return
    previewMutation.mutate()
  }, [params, templateId])

  function setParam(key: string, value: unknown) {
    setParams((current) => ({ ...current, [key]: value }))
  }

  function handleInsert() {
    onInsert(preview)
    onClose()
  }

  return (
    <Modal opened={opened} onClose={onClose} title="应用 Location 模板" size="xl">
      <Stack gap="md">
        {templatesQuery.isError ? <ErrorAlert error={templatesQuery.error} title="加载模板失败" /> : null}
        <Group justify="space-between" align="flex-end" gap="sm">
          <Select
            style={{ flex: 1 }}
            label="模板"
            value={templateId}
            data={templates.map((item) => ({ value: item.id, label: item.category ? `${item.name} · ${item.category}` : item.name }))}
            onChange={(value) => value && setTemplateId(value)}
          />
          <Button variant="subtle" leftSection={<IconSettings size={16} />} onClick={() => setManagerOpened(true)}>管理</Button>
        </Group>
        {selected ? <Text size="sm" c="dimmed">{selected.description}</Text> : null}
        {selectedParams.map((param) => param.type === 'boolean' ? (
          <Switch key={param.key} label={param.label} checked={Boolean(params[param.key])} onChange={(event) => setParam(param.key, event.currentTarget.checked)} />
        ) : (
          <TextInput key={param.key} label={param.label} value={String(params[param.key] ?? '')} onChange={(event) => setParam(param.key, event.currentTarget.value)} />
        ))}
        <Textarea label="预览" value={preview} autosize minRows={10} readOnly styles={{ input: { fontFamily: 'monospace' } }} />
        <Group justify="flex-end">
          <Button disabled={!preview} onClick={handleInsert}>插入到 Location 编辑框</Button>
        </Group>
      </Stack>
      <Suspense fallback={null}>
        <RewriteTemplateManager opened={managerOpened} onClose={() => setManagerOpened(false)} />
      </Suspense>
    </Modal>
  )
}

export default RewriteTemplateModal
