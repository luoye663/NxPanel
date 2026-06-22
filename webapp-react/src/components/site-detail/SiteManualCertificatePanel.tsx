import { Alert, Button, Divider, Group, Stack, Switch, Table, Text, Textarea } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { downloadSSLCertificate, getSSLContent, disableSSL, uploadManualPem, type SSLStatus } from '@/api/ssl'
import type { SiteDetail } from '@/api/types'
import { PathCell } from '@/components/common/PathCell'
import { TimeCell } from '@/components/common/TimeCell'
import { siteDetailKeys } from '@/hooks/useSiteDetail'
import { confirmDanger } from '@/utils/confirm'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

interface SiteManualCertificatePanelProps {
  site: SiteDetail
  ssl: SSLStatus
  active: boolean
  onChanged: () => Promise<void>
}

interface PemFormValues {
  certificate_pem: string
  private_key_pem: string
  force_https: boolean
}

function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  anchor.click()
  URL.revokeObjectURL(url)
}

export function SiteManualCertificatePanel({ site, ssl, active, onChanged }: SiteManualCertificatePanelProps) {
  const queryClient = useQueryClient()
  const contentQuery = useQuery({ queryKey: ['site-detail', site.id, 'ssl-content'], queryFn: () => getSSLContent(site.id), enabled: ssl.enabled && active })
  const uploadMutation = useMutation({ mutationFn: (values: PemFormValues) => uploadManualPem(site.id, values) })
  const disableMutation = useMutation({ mutationFn: () => disableSSL(site.id) })
  const form = useForm<PemFormValues>({
    initialValues: { certificate_pem: '', private_key_pem: '', force_https: true },
    validate: {
      certificate_pem: (value) => value.trim() ? null : '请输入证书内容',
      private_key_pem: (value) => value.trim() ? null : '请输入私钥内容',
    },
  })

  useEffect(() => {
    form.setFieldValue('force_https', ssl.force_https ?? true)
  }, [ssl.force_https])

  useEffect(() => {
    if (!ssl.enabled) {
      form.setValues({ certificate_pem: '', private_key_pem: '', force_https: ssl.force_https ?? true })
      return
    }
    if (!contentQuery.data) return
    // 私钥仅进入受控表单，不写入通知、日志或错误详情，避免敏感信息泄露。
    form.setValues({ certificate_pem: contentQuery.data.certificate_pem, private_key_pem: contentQuery.data.private_key_pem, force_https: ssl.force_https ?? true })
  }, [contentQuery.data, ssl.enabled])

  useEffect(() => {
    if (active && ssl.enabled) void contentQuery.refetch()
    // 当前证书 tab 每次激活都重新读取 PEM，避免证书夹部署后仍显示旧内容或空白。
  }, [active, ssl.enabled])

  async function handleSave(values: PemFormValues) {
    try {
      await uploadMutation.mutateAsync(values)
      notifySuccess({ message: ssl.enabled ? '证书已更新' : 'SSL 已启用' })
      await onChanged()
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['site-detail', site.id, 'ssl-content'] }),
        queryClient.invalidateQueries({ queryKey: siteDetailKeys.detail(site.id) }),
      ])
      if (active) void contentQuery.refetch()
    } catch (error) {
      showErrorModal(error, '保存证书失败')
    }
  }

  function handleDisable() {
    confirmDanger({
      title: '禁用 SSL',
      message: '禁用 SSL 后，HTTPS 将不再可用。确认禁用？',
      confirmLabel: '确认禁用',
      errorTitle: '禁用 SSL 失败',
      onConfirm: async () => {
        await disableMutation.mutateAsync()
        form.setValues({ certificate_pem: '', private_key_pem: '', force_https: true })
        await queryClient.removeQueries({ queryKey: ['site-detail', site.id, 'ssl-content'] })
        await onChanged()
        notifySuccess({ message: 'SSL 已禁用' })
      },
    })
  }

  async function handleDownload() {
    try {
      const blob = await downloadSSLCertificate(site.id)
      downloadBlob(blob, `${site.primary_domain || 'certificate'}_ssl.zip`)
    } catch (error) {
      showErrorModal(error, '下载证书失败')
    }
  }

  return (
    <Stack gap="md">
      <form onSubmit={form.onSubmit(handleSave)}>
        <Stack gap="sm" maw={760}>
          <Textarea label="证书内容 (PEM)" minRows={3} maxRows={4} autosize placeholder={ssl.enabled ? '' : '-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----'} {...form.getInputProps('certificate_pem')} />
          <Textarea label="私钥内容 (PEM)" minRows={3} maxRows={4} autosize placeholder={ssl.enabled ? '' : '-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----'} {...form.getInputProps('private_key_pem')} />
          <Switch label="强制 HTTPS" {...form.getInputProps('force_https', { type: 'checkbox' })} />
          <Group><Button type="submit" loading={uploadMutation.isPending}>保存</Button>{ssl.enabled ? <Button variant="light" onClick={handleDownload}>下载证书</Button> : null}{ssl.enabled ? <Button color="red" variant="light" loading={disableMutation.isPending} onClick={handleDisable}>关闭 SSL</Button> : null}</Group>
        </Stack>
      </form>
      {!ssl.enabled ? <Alert color="blue">请粘贴证书内容或使用其他方式部署证书。</Alert> : null}
      {ssl.enabled ? (
        <>
          <Divider />
          <Table withTableBorder className="sslInfoTable">
            <Table.Tbody>
              <Table.Tr><Table.Th>状态</Table.Th><Table.Td><Text c="green" fw={600}>已启用</Text></Table.Td><Table.Th>模式</Table.Th><Table.Td>{ssl.mode === 'from_store' ? '证书夹' : ssl.mode}</Table.Td></Table.Tr>
              <Table.Tr><Table.Th>强制 HTTPS</Table.Th><Table.Td>{ssl.force_https ? '是' : '否'}</Table.Td><Table.Th>有效期起始</Table.Th><Table.Td><TimeCell value={ssl.not_before} /></Table.Td></Table.Tr>
              <Table.Tr><Table.Th>有效期截止</Table.Th><Table.Td><TimeCell value={ssl.not_after} /></Table.Td><Table.Th>DNS Names</Table.Th><Table.Td>{ssl.dns_names?.join(', ') || '-'}</Table.Td></Table.Tr>
              <Table.Tr><Table.Th>证书路径</Table.Th><Table.Td colSpan={3}><PathCell value={ssl.cert_path} maxWidth="100%" /></Table.Td></Table.Tr>
              <Table.Tr><Table.Th>私钥路径</Table.Th><Table.Td colSpan={3}><PathCell value={ssl.key_path} maxWidth="100%" /></Table.Td></Table.Tr>
              <Table.Tr><Table.Th>签发者</Table.Th><Table.Td colSpan={3}>{ssl.issuer || '-'}</Table.Td></Table.Tr>
              <Table.Tr><Table.Th>使用者</Table.Th><Table.Td colSpan={3}>{ssl.subject || '-'}</Table.Td></Table.Tr>
            </Table.Tbody>
          </Table>
        </>
      ) : null}
    </Stack>
  )
}
