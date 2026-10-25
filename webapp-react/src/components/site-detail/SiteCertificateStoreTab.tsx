import { ActionIcon, Alert, Badge, Button, Group, Modal, Popover, Stack, Text, Textarea, TextInput, Tooltip } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { useForm } from '@mantine/form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconCloudUpload, IconInfoCircle, IconRocket, IconTrash } from '@tabler/icons-react'
import type { MRT_ColumnDef } from 'mantine-react-table'
import { useState } from 'react'
import { previewCertificateReplacement, replaceSiteCertificate, adoptSiteCertificate, scanSiteCertificate, deleteCertificate, deployCertificate, listCertificates, uploadCertificate, type CertificateItem, type CertificateReplacementPreview } from '@/api/certificates'
import type { SiteDetail } from '@/api/types'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { MonoText } from '@/components/common/MonoText'
import { PathCell } from '@/components/common/PathCell'
import { TimeCell } from '@/components/common/TimeCell'
import { DataTable } from '@/components/tables/DataTable'
import { confirmDanger } from '@/utils/confirm'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

interface SiteCertificateStoreTabProps {
  site: SiteDetail
  onChanged: () => Promise<void>
}

interface UploadFormValues {
  name: string
  certificate_pem: string
  private_key_pem: string
}

function extractOrg(issuer: string): string {
  if (!issuer) return '-'
  const match = issuer.match(/O=([^,]+)/)
  return match?.[1] || issuer
}

export function SiteCertificateStoreTab({ site, onChanged }: SiteCertificateStoreTabProps) {
  const queryClient = useQueryClient()
  const [opened, handlers] = useDisclosure(false)
  const certsQueryKey = ['certificates'] as const
  const certsQuery = useQuery({ queryKey: certsQueryKey, queryFn: listCertificates })
  const uploadMutation = useMutation({ mutationFn: uploadCertificate })
  const deployMutation = useMutation({ mutationFn: (certId: string) => deployCertificate(certId, { site_id: site.id, force_https: true }) })
  const deleteMutation = useMutation({ mutationFn: deleteCertificate })
  const scanQuery = useQuery({ queryKey: ['certificate-scan', site.id], queryFn: () => scanSiteCertificate(site.id), enabled: false, retry: false })
  const adoptMutation = useMutation({ mutationFn: (hash: string) => adoptSiteCertificate(site.id, hash) })
  const [replacement, setReplacement] = useState<{ cert: CertificateItem; preview: CertificateReplacementPreview } | null>(null)
  const previewMutation = useMutation({ mutationFn: (id: string) => previewCertificateReplacement(site.id, id) })
  const replaceMutation = useMutation({ mutationFn: () => replaceSiteCertificate(site.id, replacement!.cert.id, replacement!.preview) })
  const form = useForm<UploadFormValues>({
    initialValues: { name: '', certificate_pem: '', private_key_pem: '' },
    validate: {
      name: (value) => value.trim() ? null : '请输入证书名称',
      certificate_pem: (value) => value.trim() ? null : '请输入证书内容',
      private_key_pem: (value) => value.trim() ? null : '请输入私钥内容',
    },
  })

  const columns: MRT_ColumnDef<CertificateItem>[] = [
    { accessorKey: 'source', header: '来源', size: 110, Cell: ({ row }) => <Badge color={row.original.source === 'external' ? 'orange' : 'blue'} variant="light">{row.original.source === 'external' ? '外部纳管' : '面板管理'}</Badge> },
    { accessorKey: 'domains', header: '域名', size: 220, Cell: ({ row }) => <MonoText value={row.original.domains?.join(', ') || '-'} maxWidth={260} /> },
    { accessorKey: 'not_after', header: '到期时间', size: 180, Cell: ({ row }) => <TimeCell value={row.original.not_after} /> },
    { accessorKey: 'issuer', header: '证书品牌', size: 150, Cell: ({ row }) => extractOrg(row.original.issuer) },
    { accessorKey: 'cert_path', header: '证书位置', Cell: ({ row }) => <PathCell value={row.original.cert_path} maxWidth={260} /> },
    { id: 'associated_sites', header: '关联站点', Cell: ({ row }) => <Text size="sm">{row.original.associated_sites?.map((item) => item.primary_domain).join(', ') || '未关联'}</Text> },
  ]

  async function handleUpload(values: UploadFormValues) {
    try {
      await uploadMutation.mutateAsync({ name: values.name.trim(), certificate_pem: values.certificate_pem, private_key_pem: values.private_key_pem })
      notifySuccess({ message: '证书已上传到证书夹' })
      form.reset()
      handlers.close()
      await queryClient.invalidateQueries({ queryKey: certsQueryKey })
    } catch (error) {
      showErrorModal(error, '上传证书失败')
    }
  }

  function handleDeploy(cert: CertificateItem) {
    confirmDanger({
      title: '部署证书',
      message: `确认将证书「${cert.name}」部署到站点 ${site.primary_domain || ''}？`,
      confirmLabel: '确认部署',
      errorTitle: '部署证书失败',
      onConfirm: async () => {
        await deployMutation.mutateAsync(cert.id)
        notifySuccess({ message: '证书已部署，SSL 已启用' })
        await onChanged()
      },
    })
  }

  function handleDelete(cert: CertificateItem) {
    confirmDanger({
      title: '删除证书',
      message: cert.source === 'external'
        ? `确认取消「${cert.name}」的纳管及元数据关联？不会删除外部证书或私钥，也不会修改站点配置。`
        : `确认删除证书「${cert.name}」及面板管理的文件？仍被站点引用的证书不能删除，请先替换相关站点证书。`,
      confirmLabel: '确认删除',
      errorTitle: '删除证书失败',
      onConfirm: async () => {
        await deleteMutation.mutateAsync(cert.id)
        notifySuccess({ message: '证书已删除' })
        await queryClient.invalidateQueries({ queryKey: certsQueryKey })
      },
    })
  }

  return (
    <Stack gap="md">
      <Group gap="xs">
        <Button variant="light" loading={scanQuery.isFetching} onClick={() => { void scanQuery.refetch() }}>扫描当前站点</Button>
        <Popover width={300} position="bottom-start" withArrow shadow="md">
          <Popover.Target><ActionIcon variant="subtle" color="gray" aria-label="证书扫描说明"><IconInfoCircle size={18} /></ActionIcon></Popover.Target>
          <Popover.Dropdown><Text size="sm">扫描只读取当前站点配置。确认纳管仅建立证书夹元数据关联，不修改 Nginx、不 reload，也不会读取私钥到浏览器。复杂配置保留只读。</Text></Popover.Dropdown>
        </Popover>
      </Group>
      {scanQuery.isError ? <ErrorAlert error={scanQuery.error} title="扫描失败" /> : null}
      {scanQuery.data ? (
        <Stack gap="xs">
          {scanQuery.data.read_only_reason ? <Alert color="yellow" title="仅只读展示，未纳管">{scanQuery.data.read_only_reason}</Alert> : null}
          {scanQuery.data.cert_path ? <Stack gap={4}><Text size="sm">证书路径</Text><PathCell value={scanQuery.data.cert_path} maxWidth="100%" /></Stack> : null}
          {scanQuery.data.certificate ? <Stack gap={4}><Text size="sm" style={{ overflowWrap: 'anywhere' }}>{scanQuery.data.certificate.dns_names.join(', ')}</Text><Group gap="xs"><Text size="sm">到期时间</Text><TimeCell value={scanQuery.data.certificate.not_after} /></Group></Stack> : null}
          <Group><Button disabled={Boolean(scanQuery.data.read_only_reason) || !scanQuery.data.certificate || scanQuery.isFetching} loading={adoptMutation.isPending} onClick={async () => {
            try {
              await adoptMutation.mutateAsync(scanQuery.data!.file_hash)
              notifySuccess({ message: '已建立元数据关联，原配置和文件保持不变' })
              await queryClient.invalidateQueries({ queryKey: certsQueryKey })
            } catch (error) { showErrorModal(error, '确认纳管失败，请重新扫描') }
          }}>确认纳管（不修改配置）</Button></Group>
        </Stack>
      ) : null}
      {certsQuery.isError ? <ErrorAlert error={certsQuery.error} title="加载证书夹失败" /> : null}
      <DataTable
        columns={columns}
        data={certsQuery.data || []}
        loading={certsQuery.isLoading || certsQuery.isFetching}
        emptyText="暂无证书，请先上传证书到证书夹"
        toolbarActions={<Button leftSection={<IconCloudUpload size={16} />} onClick={handlers.open}>上传到证书夹</Button>}
        renderRowActions={({ row }) => (
          <Group gap={4} wrap="nowrap">
            <Button size="compact-xs" variant="light" disabled={row.original.source === 'external'} loading={previewMutation.isPending} onClick={async () => { try { const preview = await previewMutation.mutateAsync(row.original.id); setReplacement({ cert: row.original, preview }) } catch (error) { showErrorModal(error, '无法安全局部替换') } }}>预览局部替换</Button>
            <Tooltip label={row.original.source === 'external' || site.is_imported || site.maintenance_enabled ? '请使用预览局部替换，不执行 marker 部署' : '部署到站点'}><ActionIcon aria-label="部署到站点" disabled={row.original.source === 'external' || site.is_imported || site.maintenance_enabled} variant="subtle" loading={deployMutation.isPending} onClick={() => handleDeploy(row.original)}><IconRocket size={16} /></ActionIcon></Tooltip>
            <Tooltip label={row.original.source === 'external' ? '取消纳管（保留文件）' : '删除'}><ActionIcon aria-label={row.original.source === 'external' ? '取消纳管' : '删除证书'} color="red" variant="subtle" loading={deleteMutation.isPending} onClick={() => handleDelete(row.original)}><IconTrash size={16} /></ActionIcon></Tooltip>
          </Group>
        )}
      />
      <Modal opened={Boolean(replacement)} onClose={() => { if (!replaceMutation.isPending) setReplacement(null) }} title="确认局部替换证书引用" size="lg" closeOnClickOutside={false}>
        {replacement ? <Stack gap="md">
          <Alert color="blue">只修改目标 server 的证书与私钥路径引用。原外部文件、HTTPS 监听、维护块和业务配置保持不变；应用前校验哈希，执行 nginx -t 和 reload。</Alert>
          <Text size="sm">证书引用差异</Text>
          <PathCell value={replacement.preview.old_cert_path} maxWidth="100%" />
          <Text size="sm">替换为</Text><PathCell value={replacement.preview.new_cert_path} maxWidth="100%" />
          <Text size="sm">私钥引用差异（仅路径，不读取私钥内容）</Text>
          <PathCell value={replacement.preview.old_key_path} maxWidth="100%" />
          <Text size="sm">替换为</Text><PathCell value={replacement.preview.new_key_path} maxWidth="100%" />
          <Group justify="flex-end"><Button variant="default" disabled={replaceMutation.isPending} onClick={() => setReplacement(null)}>取消</Button><Button loading={replaceMutation.isPending} onClick={async () => {
            try { await replaceMutation.mutateAsync(); notifySuccess({ message: '证书引用已局部替换，原文件保留' }); setReplacement(null); await Promise.all([onChanged(), queryClient.invalidateQueries({ queryKey: certsQueryKey }), queryClient.invalidateQueries({ queryKey: ['maintenance', site.id] })]) }
            catch (error) { showErrorModal(error, '替换未能确认，请重新扫描对账') }
          }}>确认应用</Button></Group>
        </Stack> : null}
      </Modal>
      <Modal opened={opened} onClose={handlers.close} title="上传证书到证书夹" size="lg" closeOnClickOutside={false} centered>
        <form onSubmit={form.onSubmit(handleUpload)}>
          <Stack gap="md">
            <TextInput label="证书名称" placeholder="例如: example.com" {...form.getInputProps('name')} />
            <Textarea label="证书内容 (PEM)" styles={{ input: { height: 180, resize: 'none', overflowY: 'auto' } }} placeholder="-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----" {...form.getInputProps('certificate_pem')} />
            <Textarea label="私钥内容 (PEM)" styles={{ input: { height: 180, resize: 'none', overflowY: 'auto' } }} placeholder="-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----" {...form.getInputProps('private_key_pem')} />
            {/* 证书私钥只提交给后端保存，前端不做日志输出或本地持久化。 */}
            <Group justify="flex-end"><Button variant="default" onClick={handlers.close}>取消</Button><Button type="submit" loading={uploadMutation.isPending}>上传</Button></Group>
          </Stack>
        </form>
      </Modal>
    </Stack>
  )
}
