import { Alert, Button, Checkbox, Divider, Group, NumberInput, SegmentedControl, Stack, Switch, Text, TextInput, Textarea } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, type ReactNode } from 'react'
import { updateSite, updateSiteDocument } from '@/api/sites'
import type { Binding, SiteDetail } from '@/api/types'
import { PathCell } from '@/components/common/PathCell'
import { SectionCard } from '@/components/common/SectionCard'
import { siteDetailKeys } from '@/hooks/useSiteDetail'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

interface SiteBasicTabProps {
  site: SiteDetail
}

interface BasicFormValues {
  bindingsText: string
  https_port: number
  root_path: string
  index_files: string
  access_log_enabled: boolean
}

interface DocumentFormValues {
  indexText: string
  autoindex_enabled: boolean
  autoindex_exact_size: boolean
  autoindex_localtime: boolean
  autoindex_format: string
  error_page_404: string
  error_page_403: string
}

function bindingsToText(bindings: Binding[]): string {
  return (bindings || []).map((binding) => binding.port === 80 ? binding.domain : `${binding.domain}:${binding.port}`).join('\n')
}

function parseBindings(value: string): Binding[] {
  const bindings: Binding[] = []
  for (const line of value.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed) continue
    const colonIndex = trimmed.lastIndexOf(':')
    if (colonIndex > 0) {
      const domain = trimmed.slice(0, colonIndex).trim()
      const port = Number.parseInt(trimmed.slice(colonIndex + 1).trim(), 10)
      if (domain && Number.isInteger(port) && port > 0 && port <= 65535) {
        bindings.push({ domain, port })
        continue
      }
    }
    bindings.push({ domain: trimmed, port: 80 })
  }
  return bindings
}

function isBasicMarkerMissing(site: SiteDetail): boolean {
  const missing = site.marker_status?.missing || []
  return ['LISTEN', 'SERVER-NAME', 'ROOT', 'LOG'].some((marker) => missing.includes(marker))
}

function isDocumentMarkerMissing(site: SiteDetail): boolean {
  return (site.marker_status?.missing || []).includes('DOCUMENT')
}

function InfoItem({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="siteDetailInfoRow">
      <Text size="xs" c="dimmed">{label}</Text>
      <div>{children}</div>
    </div>
  )
}

export function SiteBasicTab({ site }: SiteBasicTabProps) {
  const queryClient = useQueryClient()
  const markerMissing = isBasicMarkerMissing(site)
  const form = useForm<BasicFormValues>({
    initialValues: {
      bindingsText: bindingsToText(site.bindings),
      https_port: site.https_port || 443,
      root_path: site.root_path || '',
      index_files: site.index_files || '',
      access_log_enabled: site.access_log_enabled,
    },
    validate: {
      bindingsText: (value) => parseBindings(value).length === 0 ? '至少需要一个域名绑定' : null,
      https_port: (value) => value < 1 || value > 65535 ? '端口范围必须为 1-65535' : null,
      root_path: (value) => value.trim() ? null : '请输入网站根目录',
      index_files: (value) => value.trim() ? null : '请输入默认首页',
    },
  })

  const updateMutation = useMutation({
    mutationFn: (values: BasicFormValues) => updateSite(site.id, {
      bindings: parseBindings(values.bindingsText),
      https_port: values.https_port,
      root_path: values.root_path.trim(),
      index_files: values.index_files.trim(),
      access_log_enabled: values.access_log_enabled,
    }),
  })

  useEffect(() => {
    // 切换站点或重新拉取详情时同步表单，避免把上一站点未保存表单带到当前站点。
    form.setValues({
      bindingsText: bindingsToText(site.bindings),
      https_port: site.https_port || 443,
      root_path: site.root_path || '',
      index_files: site.index_files || '',
      access_log_enabled: site.access_log_enabled,
    })
    form.clearErrors()
  }, [site.id])

  async function handleSave(values: BasicFormValues) {
    try {
      await updateMutation.mutateAsync(values)
      notifySuccess({ message: '基础配置已保存' })
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: siteDetailKeys.detail(site.id) }),
        queryClient.invalidateQueries({ queryKey: ['sites'] }),
      ])
    } catch (error) {
      showErrorModal(error, '保存基础配置失败')
    }
  }

  return (
    <SectionCard>
      <form onSubmit={form.onSubmit(handleSave)}>
        <Stack gap="md" maw={760}>
          {markerMissing ? (
            <Alert color="red" title="基础配置标识块缺失">
              该站点配置文件缺少 NxPanel 基础配置标识块，表单保存可能无法安全修改对应片段。请先在「站点配置」中检查并修复。
            </Alert>
          ) : null}
          <Textarea label="域名绑定" minRows={5} autosize placeholder={'example.com\napi.example.com:8608'} description="每行一个域名，可用 域名:端口 指定端口（默认 80）。第一行为主域名。" {...form.getInputProps('bindingsText')} />
          <NumberInput label="HTTPS 端口" min={1} max={65535} allowDecimal={false} {...form.getInputProps('https_port')} />
          <TextInput label="根目录" {...form.getInputProps('root_path')} />
          <TextInput label="默认首页" description="多个首页文件沿用 Nginx index 指令写法，例如 index.html index.htm。" {...form.getInputProps('index_files')} />
          <Checkbox label="启用访问日志" {...form.getInputProps('access_log_enabled', { type: 'checkbox' })} />
          <Group justify="flex-end"><Button type="submit" loading={updateMutation.isPending}>保存配置</Button></Group>
        </Stack>
      </form>
      <Divider my="md" label="日志路径" labelPosition="left" />
      <div className="siteDetailInfoPanel">
        <InfoItem label="Access Log 路径"><PathCell value={site.access_log_path} maxWidth="100%" /></InfoItem>
        <InfoItem label="Error Log 路径"><PathCell value={site.error_log_path} maxWidth="100%" /></InfoItem>
      </div>
    </SectionCard>
  )
}

export function SiteDocumentTab({ site }: SiteBasicTabProps) {
  const queryClient = useQueryClient()
  const documentMarkerMissing = isDocumentMarkerMissing(site)
  const documentForm = useForm<DocumentFormValues>({
    initialValues: {
      indexText: (site.index_file_list?.length ? site.index_file_list : site.index_files.split(/\s+/)).filter(Boolean).join('\n'),
      autoindex_enabled: site.autoindex_enabled,
      autoindex_exact_size: site.autoindex_exact_size,
      autoindex_localtime: site.autoindex_localtime,
      autoindex_format: site.autoindex_format || 'html',
      error_page_404: site.error_page_404 || '',
      error_page_403: site.error_page_403 || '',
    },
    validate: {
      indexText: (value) => value.split('\n').some((line) => line.trim()) ? null : '至少需要一个默认首页文件',
      error_page_404: (value) => !value.trim() || value.trim().startsWith('/') ? null : '请填写站点内 URI，例如 /404.html',
      error_page_403: (value) => !value.trim() || value.trim().startsWith('/') ? null : '请填写站点内 URI，例如 /403.html',
    },
  })

  const documentMutation = useMutation({
    mutationFn: (values: DocumentFormValues) => updateSiteDocument(site.id, {
      index_files: values.indexText.split('\n').map((line) => line.trim()).filter(Boolean),
      autoindex_enabled: values.autoindex_enabled,
      autoindex_exact_size: values.autoindex_exact_size,
      autoindex_localtime: values.autoindex_localtime,
      autoindex_format: values.autoindex_format,
      error_page_404: values.error_page_404.trim(),
      error_page_403: values.error_page_403.trim(),
    }),
  })

  useEffect(() => {
    // 切换站点或重新拉取详情时同步表单。
    documentForm.setValues({
      indexText: (site.index_file_list?.length ? site.index_file_list : site.index_files.split(/\s+/)).filter(Boolean).join('\n'),
      autoindex_enabled: site.autoindex_enabled,
      autoindex_exact_size: site.autoindex_exact_size,
      autoindex_localtime: site.autoindex_localtime,
      autoindex_format: site.autoindex_format || 'html',
      error_page_404: site.error_page_404 || '',
      error_page_403: site.error_page_403 || '',
    })
    documentForm.clearErrors()
  }, [site.id])

  async function handleDocumentSave(values: DocumentFormValues) {
    try {
      await documentMutation.mutateAsync(values)
      notifySuccess({ message: '默认文档配置已保存' })
      await queryClient.invalidateQueries({ queryKey: siteDetailKeys.detail(site.id) })
    } catch (error) {
      showErrorModal(error, '保存默认文档配置失败')
    }
  }

  return (
    <SectionCard>
      <form onSubmit={documentForm.onSubmit(handleDocumentSave)}>
        <Stack gap="md" maw={760}>
          {documentMarkerMissing ? (
            <Alert color="yellow" title="文档增强标识块缺失">
              当前配置缺少 DOCUMENT marker，不能安全写入 autoindex 和 error_page。请先在「站点配置」执行 marker 迁移或手动补齐标识块。
            </Alert>
          ) : null}
          <Textarea label="默认首页顺序" minRows={4} description="每行一个文件名，保存时会按当前顺序写入 Nginx index 指令。" {...documentForm.getInputProps('indexText')} />
          <Switch label="开启目录浏览 autoindex" description="仅建议临时排查文件时开启，公开目录可能暴露文件列表。公开目录建议保持关闭。" {...documentForm.getInputProps('autoindex_enabled', { type: 'checkbox' })} />
          {documentForm.values.autoindex_enabled ? (
            <Stack gap="sm">
              <Switch label="精确显示文件大小" description="关闭时 Nginx 会以 KB/MB/GB 等易读单位显示文件大小。" {...documentForm.getInputProps('autoindex_exact_size', { type: 'checkbox' })} />
              <Switch label="使用本地时间" description="关闭时目录列表使用 UTC 时间。" {...documentForm.getInputProps('autoindex_localtime', { type: 'checkbox' })} />
              <SegmentedControl
                data={[
                  { label: 'HTML', value: 'html' },
                  { label: 'JSON', value: 'json' },
                  { label: 'XML', value: 'xml' },
                  { label: 'JSONP', value: 'jsonp' },
                ]}
                {...documentForm.getInputProps('autoindex_format')}
              />
            </Stack>
          ) : null}
          <TextInput label="404 错误页 URI" placeholder="/404.html" {...documentForm.getInputProps('error_page_404')} />
          <TextInput label="403 错误页 URI" placeholder="/403.html" {...documentForm.getInputProps('error_page_403')} />
          {/* 错误页只保存站点内 URI，实际 HTML 文件可通过站点文件管理维护。 */}
          <Group justify="flex-end"><Button type="submit" loading={documentMutation.isPending} disabled={documentMarkerMissing}>保存默认文档</Button></Group>
        </Stack>
      </form>
    </SectionCard>
  )
}
