import { ActionIcon, Autocomplete, Badge, Button, Group, Modal, NumberInput, Select, Stack, Switch, Table, Text, TextInput, Textarea } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconArrowLeft, IconEdit, IconPlus, IconTrash } from '@tabler/icons-react'
import { lazy, Suspense, useEffect, useState, type ReactNode } from 'react'
import {
  createRewriteTemplate,
  deleteRewriteTemplate,
  getRewriteTemplates,
  updateRewriteTemplate,
  type RewriteTemplateInput,
  type RewriteTemplateItem,
  type RewriteTemplateParam,
} from '@/api/rewrite'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { confirmDanger } from '@/utils/confirm'
import { notifyError, notifySuccess } from '@/utils/notify'

const NginxCodeEditor = lazy(() => import('@/components/editor/NginxCodeEditor'))

interface RewriteTemplateManagerProps {
  opened: boolean
  onClose: () => void
}

const TEMPLATES_KEY = ['rewrite-templates'] as const
const PARAM_TYPES = [
  { value: 'string', label: '字符串' },
  { value: 'number', label: '数字' },
  { value: 'boolean', label: '布尔' },
  { value: 'select', label: '下拉选择' },
]
const emptyParam = (): RewriteTemplateParam => ({ key: '', label: '', type: 'string', default: '', required: false, options: [] })

type EditorState = { mode: 'create' } | { mode: 'edit'; template: RewriteTemplateItem } | null

export function RewriteTemplateManager({ opened, onClose }: RewriteTemplateManagerProps) {
  const queryClient = useQueryClient()
  const templatesQuery = useQuery({ queryKey: TEMPLATES_KEY, queryFn: getRewriteTemplates, enabled: opened })
  const [editor, setEditor] = useState<EditorState>(null)

  function invalidate() {
    return queryClient.invalidateQueries({ queryKey: TEMPLATES_KEY })
  }

  function handleClose() {
    setEditor(null)
    onClose()
  }

  return (
    <>
      {editor ? (
        <TemplateEditor
          state={editor}
          categories={deriveCategories(templatesQuery.data?.templates)}
          onClose={() => setEditor(null)}
          onSaved={async () => {
            await invalidate()
            setEditor(null)
          }}
        />
      ) : (
        <TemplateList
          opened={opened}
          onClose={handleClose}
          items={templatesQuery.data?.templates ?? []}
          isLoading={templatesQuery.isLoading}
          isError={templatesQuery.isError}
          error={templatesQuery.error}
          onCreate={() => setEditor({ mode: 'create' })}
          onEdit={(template) => setEditor({ mode: 'edit', template })}
          onDeleted={invalidate}
          onToggleEnabled={async (template, enabled) => {
            try {
              await updateRewriteTemplate(template.id, toInput({ ...template, enabled }))
              notifySuccess({ message: enabled ? '已启用' : '已禁用' })
              await invalidate()
            } catch (error) {
              notifyError({ message: '切换状态失败' })
              throw error
            }
          }}
        />
      )}
    </>
  )
}

interface TemplateListProps {
  opened: boolean
  onClose: () => void
  items: RewriteTemplateItem[]
  isLoading: boolean
  isError: boolean
  error: unknown
  onCreate: () => void
  onEdit: (template: RewriteTemplateItem) => void
  onDeleted: () => void | Promise<void>
  onToggleEnabled: (template: RewriteTemplateItem, enabled: boolean) => void | Promise<void>
}

function TemplateList({ opened, onClose, items, isLoading, isError, error, onCreate, onEdit, onDeleted, onToggleEnabled }: TemplateListProps) {
  const rows = items.map((template) => (
    <Table.Tr key={template.id}>
      <Table.Td>
        <Stack gap={2}>
          <Text fw={500} size="sm">{template.name}</Text>
          {template.description ? <Text size="xs" c="dimmed" lineClamp={1}>{template.description}</Text> : null}
        </Stack>
      </Table.Td>
      <Table.Td>{template.category ? <Badge variant="light">{template.category}</Badge> : <Text size="xs" c="dimmed">—</Text>}</Table.Td>
      <Table.Td><Text size="xs" c="dimmed">{template.params.length} 项</Text></Table.Td>
      <Table.Td>
        <Switch checked={template.enabled} onChange={(e) => onToggleEnabled(template, e.currentTarget.checked)} aria-label="启用" />
      </Table.Td>
      <Table.Td>
        <Group gap="xs">
          <ActionIcon variant="subtle" color="blue" title="编辑" onClick={() => onEdit(template)}>
            <IconEdit size={16} />
          </ActionIcon>
          <ActionIcon color="red" variant="subtle" title="删除" onClick={() => handleDelete(template)}>
            <IconTrash size={16} />
          </ActionIcon>
        </Group>
      </Table.Td>
    </Table.Tr>
  ))

  function handleDelete(template: RewriteTemplateItem) {
    confirmDanger({
      title: '删除模板',
      message: `确定删除模板「${template.name}」？此操作不可撤销。`,
      confirmLabel: '删除',
      errorTitle: '删除模板失败',
      onConfirm: async () => {
        await deleteRewriteTemplate(template.id)
        notifySuccess({ message: '模板已删除' })
        await onDeleted()
      },
    })
  }

  return (
    <ModalListShell opened={opened} onClose={onClose} isLoading={isLoading}>
      <Stack gap="md">
        <Group justify="space-between" align="center">
          <Text size="sm" c="dimmed">共 {items.length} 个模板。禁用的模板不会出现在应用列表。</Text>
          <Button leftSection={<IconPlus size={16} />} onClick={onCreate}>新增模板</Button>
        </Group>
        {isError ? <ErrorAlert error={error} title="加载模板失败" /> : null}
        <Table striped highlightOnHover>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>名称</Table.Th>
              <Table.Th>分类</Table.Th>
              <Table.Th>参数</Table.Th>
              <Table.Th>启用</Table.Th>
              <Table.Th>操作</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>{rows}</Table.Tbody>
        </Table>
      </Stack>
    </ModalListShell>
  )
}

function ModalListShell({ opened, onClose, isLoading, children }: { opened: boolean; onClose: () => void; isLoading: boolean; children: ReactNode }) {
  return (
    <Modal opened={opened} onClose={onClose} title="管理 Location 模板" size="xl" centered>
      {isLoading ? <Text size="sm" c="dimmed">加载中...</Text> : children}
    </Modal>
  )
}

interface EditorProps {
  state: Exclude<EditorState, null>
  categories: string[]
  onClose: () => void
  onSaved: () => void | Promise<void>
}

function TemplateEditor({ state, categories, onClose, onSaved }: EditorProps) {
  const isEdit = state.mode === 'edit'
  const existing = isEdit ? state.template : null

  const [name, setName] = useState(existing?.name ?? '')
  const [category, setCategory] = useState(existing?.category ?? '')
  const [description, setDescription] = useState(existing?.description ?? '')
  const [enabled, setEnabled] = useState(existing?.enabled ?? true)
  const [sortOrder, setSortOrder] = useState(existing?.sort_order ?? 0)
  const [body, setBody] = useState(existing?.template ?? 'location / {\n    \n}\n')
  const [params, setParams] = useState<RewriteTemplateParam[]>(existing?.params?.length ? existing.params.map(normalizeParam) : [])

  useEffect(() => {
    // 编辑模式切换到不同模板时重置
    if (state.mode === 'edit') {
      setName(state.template.name)
      setCategory(state.template.category)
      setDescription(state.template.description)
      setEnabled(state.template.enabled)
      setSortOrder(state.template.sort_order)
      setBody(state.template.template)
      setParams(state.template.params.map(normalizeParam))
    }
  }, [state])

  const saveMutation = useMutation({
    mutationFn: async () => {
      const input = buildInput()
      if (isEdit) {
        return updateRewriteTemplate(state.template.id, input)
      }
      return createRewriteTemplate(input)
    },
  })

  function buildInput(): RewriteTemplateInput {
    return {
      name: name.trim(),
      category: category.trim(),
      description: description.trim(),
      template: body,
      enabled,
      sort_order: sortOrder,
      params: params.map((p) => ({
        key: p.key.trim(),
        label: p.label.trim(),
        type: p.type,
        default: p.default,
        required: p.required,
        options: p.type === 'select' ? p.options : undefined,
      })),
    }
  }

  function setParam(index: number, patch: Partial<RewriteTemplateParam>) {
    setParams((current) => current.map((p, i) => (i === index ? { ...p, ...patch } : p)))
  }

  function addParam() {
    setParams((current) => [...current, emptyParam()])
  }

  function removeParam(index: number) {
    setParams((current) => current.filter((_, i) => i !== index))
  }

  async function handleSave() {
    try {
      await saveMutation.mutateAsync()
      notifySuccess({ message: isEdit ? '模板已更新' : '模板已创建' })
      await onSaved()
    } catch (error) {
      notifyError({ message: error instanceof Error ? error.message : '保存失败' })
    }
  }

  return (
    <Modal opened onClose={onClose} title={isEdit ? '编辑模板' : '新增模板'} size="xl" centered>
      <Stack gap="md">
        <Group grow>
          <TextInput label="名称" required value={name} onChange={(e) => setName(e.currentTarget.value)} placeholder="如：/api 反向代理" />
          <Autocomplete label="分类" value={category} onChange={setCategory} data={categories} placeholder="如：cms" limit={20} />
        </Group>
        <Textarea label="描述" autosize minRows={1} value={description} onChange={(e) => setDescription(e.currentTarget.value)} placeholder="模板用途说明" />
        <Group gap="lg">
          <Switch label="启用" checked={enabled} onChange={(e) => setEnabled(e.currentTarget.checked)} />
          <NumberInput label="排序" w={140} value={sortOrder} onChange={(v) => setSortOrder(typeof v === 'number' ? v : 0)} />
        </Group>

        <Stack gap="xs">
          <Group justify="space-between" align="center">
            <Text fw={500} size="sm">参数</Text>
            <Button size="compact-sm" variant="light" leftSection={<IconPlus size={14} />} onClick={addParam}>添加参数</Button>
          </Group>
          <Text size="xs" c="dimmed">在模板体中使用 <Text span ff="monospace">{`{{ .参数key }}`}</Text> 引用；布尔用 <Text span ff="monospace">{`{{- if .key }}`}</Text>。</Text>
          {params.length === 0 ? <Text size="xs" c="dimmed">无参数。无参数模板可直接在下方编辑模板体。</Text> : null}
          {params.map((param, index) => (
            <ParamRow key={index} param={param} onChange={(patch) => setParam(index, patch)} onRemove={() => removeParam(index)} />
          ))}
        </Stack>

        <Stack gap="xs">
          <Text fw={500} size="sm">模板内容</Text>
          <Text size="xs" c="dimmed">Nginx server 级 Location 片段，保存时不执行 nginx -t；应用到站点时才会校验。</Text>
          <div className="nginxEditorShell" style={{ minHeight: 240 }}>
            <Suspense fallback={<Text size="sm" c="dimmed">加载编辑器...</Text>}>
              <NginxCodeEditor value={body} onChange={setBody} />
            </Suspense>
          </div>
        </Stack>

        <Group justify="flex-end">
          <Button variant="default" leftSection={<IconArrowLeft size={16} />} onClick={onClose}>取消</Button>
          <Button loading={saveMutation.isPending} disabled={!name.trim() || !body.trim()} onClick={handleSave}>保存</Button>
        </Group>
      </Stack>
    </Modal>
  )
}

interface ParamRowProps {
  param: RewriteTemplateParam
  onChange: (patch: Partial<RewriteTemplateParam>) => void
  onRemove: () => void
}

function ParamRow({ param, onChange, onRemove }: ParamRowProps) {
  const optionsText = Array.isArray(param.options) ? param.options.join(',') : ''
  return (
    <Stack gap="xs" p="sm" style={{ border: '1px solid var(--mantine-color-default-border)', borderRadius: 8 }}>
      <Group grow align="flex-end">
        <TextInput label="Key" required value={param.key} onChange={(e) => onChange({ key: e.currentTarget.value })} placeholder="upstream_url" />
        <TextInput label="Label" required value={param.label} onChange={(e) => onChange({ label: e.currentTarget.value })} placeholder="目标地址" />
        <Select label="类型" data={PARAM_TYPES} value={param.type} onChange={(v) => onChange({ type: (PARAM_TYPES.some((o) => o.value === v) ? v : 'string') as RewriteTemplateParam['type'] })} />
        <Button size="sm" color="red" variant="subtle" leftSection={<IconTrash size={14} />} onClick={onRemove}>移除</Button>
      </Group>
      <Group grow align="flex-end">
        {param.type === 'boolean' ? (
          <Switch label="默认值" checked={Boolean(param.default)} onChange={(e) => onChange({ default: e.currentTarget.checked })} />
        ) : (
          <TextInput label="默认值" value={String(param.default ?? '')} onChange={(e) => onChange({ default: e.currentTarget.value })} />
        )}
        <Switch label="必填" checked={param.required} onChange={(e) => onChange({ required: e.currentTarget.checked })} />
        {param.type === 'select' ? (
          <TextInput label="选项（逗号分隔）" value={optionsText} onChange={(e) => onChange({ options: e.currentTarget.value.split(',').map((s) => s.trim()).filter(Boolean) })} placeholder="http,https" />
        ) : <div />}
      </Group>
    </Stack>
  )
}

function toInput(template: RewriteTemplateItem): RewriteTemplateInput {
  return {
    name: template.name,
    category: template.category,
    description: template.description,
    template: template.template,
    enabled: template.enabled,
    sort_order: template.sort_order,
    params: template.params,
  }
}

// deriveCategories 从已有模板派生去重、排序后的分类列表，供 Autocomplete 建议
function deriveCategories(templates: RewriteTemplateItem[] | undefined): string[] {
  if (!templates || templates.length === 0) return []
  const set = new Set<string>()
  for (const t of templates) {
    const c = t.category?.trim()
    if (c) set.add(c)
  }
  return Array.from(set).sort()
}

function normalizeParam(p: RewriteTemplateParam): RewriteTemplateParam {
  return {
    key: p.key ?? '',
    label: p.label ?? '',
    type: p.type ?? 'string',
    default: p.default ?? '',
    required: Boolean(p.required),
    options: Array.isArray(p.options) ? p.options : [],
  }
}

export default RewriteTemplateManager
