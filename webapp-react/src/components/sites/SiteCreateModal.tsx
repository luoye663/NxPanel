import { Button, Divider, Modal, Stack, Switch, TextInput, Textarea } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useEffect, useState } from 'react'
import { createSite } from '@/api/sites'
import type { Binding } from '@/api/types'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess, notifyWarning } from '@/utils/notify'

interface SiteCreateModalProps {
  opened: boolean
  onClose: () => void
  onCreated: (siteId: string) => void
}

function parseBindings(text: string): Binding[] {
  const bindings: Binding[] = []
  for (const line of text.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed) continue
    const colonIdx = trimmed.lastIndexOf(':')
    if (colonIdx > 0) {
      const domain = trimmed.substring(0, colonIdx).trim()
      const port = Number.parseInt(trimmed.substring(colonIdx + 1).trim(), 10)
      if (domain && Number.isInteger(port) && port > 0 && port <= 65535) {
        bindings.push({ domain, port })
        continue
      }
    }
    bindings.push({ domain: trimmed, port: 80 })
  }
  return bindings
}

function firstDomain(bindingsText: string): string {
  const first = bindingsText.split('\n').find((line) => line.trim())?.trim() ?? ''
  const colonIdx = first.lastIndexOf(':')
  return colonIdx > 0 ? first.substring(0, colonIdx).trim() : first
}

export function SiteCreateModal({ opened, onClose, onCreated }: SiteCreateModalProps) {
  const [autoRootPath, setAutoRootPath] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const form = useForm({
    initialValues: {
      bindingsText: '',
      root_path: '/www/wwwroot/',
      index_files: 'index.html index.htm',
      access_log_enabled: true,
      create_root: true,
      create_index: true,
      enable_after_create: true,
    },
    validate: {
      bindingsText: (value) => (value.trim() ? null : '请输入域名绑定'),
      root_path: (value) => (value.trim() ? null : '请输入网站根目录'),
    },
  })

  useEffect(() => {
    if (!opened) return
    setAutoRootPath(true)
    form.setValues({
      bindingsText: '',
      root_path: '/www/wwwroot/',
      index_files: 'index.html index.htm',
      access_log_enabled: true,
      create_root: true,
      create_index: true,
      enable_after_create: true,
    })
    form.clearErrors()
    // 每次打开弹窗都回到默认表单，避免上一次未提交内容误带入新站点。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [opened])

  useEffect(() => {
    if (!autoRootPath) return
    const domain = firstDomain(form.values.bindingsText)
    form.setFieldValue('root_path', domain ? `/www/wwwroot/${domain}` : '/www/wwwroot/')
    // root_path 是由 bindingsText 推导，依赖整个 form 对象会导致重复触发。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [autoRootPath, form.values.bindingsText])

  async function handleSubmit(values: typeof form.values) {
    const bindings = parseBindings(values.bindingsText)
    if (bindings.length === 0) {
      notifyWarning({ message: '请输入至少一个有效域名' })
      return
    }

    setSubmitting(true)
    try {
      const result = await createSite({
        bindings,
        root_path: values.root_path,
        index_files: values.index_files,
        access_log_enabled: values.access_log_enabled,
        create_root: values.create_root,
        create_index: values.create_index,
        enable_after_create: values.enable_after_create,
      })
      notifySuccess({ message: '网站创建成功' })
      onCreated(result.site_id)
    } catch (error) {
      showErrorModal(error, '创建网站失败')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal opened={opened} onClose={onClose} title="新建网站" size="lg" closeOnClickOutside={false}>
      <form onSubmit={form.onSubmit(handleSubmit)}>
        <Stack gap="md">
          <Divider label="域名绑定" labelPosition="left" />
          <Textarea
            label="域名列表"
            description="每行一个域名，可用 域名:端口 指定端口（默认 80）。第一行为主域名。"
            autosize
            minRows={5}
            placeholder={'example.com\napi.example.com:8608\nstatic.example.com:3000'}
            {...form.getInputProps('bindingsText')}
          />
          <Divider label="网站目录" labelPosition="left" />
          <TextInput
            label="网站根目录"
            description="根目录会跟随第一行域名自动生成；手动编辑后退出自动模式。"
            placeholder="/www/wwwroot/"
            {...form.getInputProps('root_path')}
            onChange={(event) => {
              setAutoRootPath(false)
              form.getInputProps('root_path').onChange(event)
            }}
          />
          <TextInput label="默认首页" placeholder="index.html index.htm" {...form.getInputProps('index_files')} />
          <Divider label="创建选项" labelPosition="left" />
          <Stack gap="xs">
            <Switch label="访问日志" {...form.getInputProps('access_log_enabled', { type: 'checkbox' })} />
            <Switch label="自动创建根目录" {...form.getInputProps('create_root', { type: 'checkbox' })} />
            <Switch label="创建默认首页" {...form.getInputProps('create_index', { type: 'checkbox' })} />
            <Switch label="创建后启用" {...form.getInputProps('enable_after_create', { type: 'checkbox' })} />
          </Stack>
          <Stack gap="xs" align="flex-end">
            <Button type="submit" loading={submitting}>创建网站</Button>
          </Stack>
        </Stack>
      </form>
    </Modal>
  )
}
