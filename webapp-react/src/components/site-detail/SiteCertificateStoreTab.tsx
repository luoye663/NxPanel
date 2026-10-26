import { ActionIcon, Alert, Badge, Button, Divider, Group, Modal, SimpleGrid, Stack, Text, Textarea, TextInput, Title, Tooltip } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { useForm } from '@mantine/form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconCloudUpload, IconRocket, IconTrash } from '@tabler/icons-react'
import type { MRT_ColumnDef } from 'mantine-react-table'
import { useState } from 'react'
import { previewCertificateReplacement, replaceSiteCertificate, adoptSiteCertificate, scanSiteCertificate, deleteCertificate, deployCertificate, listCertificates, uploadCertificate, type CertificateItem, type CertificateScan, type CertificateReplacementPreview } from '@/api/certificates'
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
  const scanQuery = useQuery({ queryKey: ['certificate-scan', site.id], queryFn: () => scanSiteCertificate(site.id), enabled: Boolean(site.is_imported || site.maintenance_enabled || site.maintenance_pending), retry: false, refetchOnMount: 'always', refetchOnWindowFocus: false })
  const adoptMutation = useMutation({ mutationFn: (hash: string) => adoptSiteCertificate(site.id, hash) })
  const [replacement, setReplacement] = useState<{ cert: CertificateItem; preview: CertificateReplacementPreview } | null>(null)
  const previewMutation = useMutation({ mutationFn: (id: string) => previewCertificateReplacement(site.id, id) })
  const replaceMutation = useMutation({ mutationFn: () => replaceSiteCertificate(site.id, replacement!.cert.id, replacement!.preview) })
  const scan = scanQuery.data
  const busy = adoptMutation.isPending || replaceMutation.isPending || deleteMutation.isPending || previewMutation.isPending || deployMutation.isPending
  const adoptionStatus = scanQuery.isError ? 'unknown' : scan?.adoption_status
  const statusLabel = { unadopted: '未接管', adopted: '已接管', changed: '待更新', unknown: '无法确认' }

  async function refreshCertificateState() {
    await Promise.all([
      onChanged(),
      queryClient.invalidateQueries({ queryKey: certsQueryKey }),
      queryClient.invalidateQueries({ queryKey: ['maintenance', site.id] }),
      scanQuery.refetch(),
    ])
  }

  async function handleScan() {
    adoptMutation.reset()
    await scanQuery.refetch()
  }

  async function handleAdopt() {
    if (!scan) return
    try {
      const result = await adoptMutation.mutateAsync(scan.file_hash)
      queryClient.setQueryData<CertificateScan>(['certificate-scan', site.id], {
        ...scan, certificate_id: result.certificate_id, adoption_status: 'adopted',
      })
      replaceMutation.reset()
      notifySuccess({ message: '已接管当前证书，原配置和文件保持不变' })
      await refreshCertificateState()
    } catch { /* Mutation errors are displayed in the current section. */ }
  }

  async function handlePreview(cert: CertificateItem) {
    replaceMutation.reset()
    try {
      const preview = await previewMutation.mutateAsync(cert.id)
      setReplacement({ cert, preview })
    } catch { /* Keep preview errors beside the certificate store. */ }
  }

  async function handleReplace() {
    try {
      await replaceMutation.mutateAsync()
      notifySuccess({ message: '证书引用已局部替换，原文件保留' })
      setReplacement(null)
      await refreshCertificateState()
    } catch { /* Never allow a failed preview to be submitted again. */ }
  }

  const form = useForm<UploadFormValues>({
    initialValues: { name: '', certificate_pem: '', private_key_pem: '' },
    validate: {
      name: (value) => value.trim() ? null : '请输入证书名称',
      certificate_pem: (value) => value.trim() ? null : '请输入证书内容',
      private_key_pem: (value) => value.trim() ? null : '请输入私钥内容',
    },
  })

  const columns: MRT_ColumnDef<CertificateItem>[] = [
    { accessorKey: 'source', header: '来源', size: 180, minSize: 170, Cell: ({ row }) => <Group gap={6} wrap="nowrap">
      <Badge color={row.original.source === 'external' ? 'orange' : 'blue'} variant="light">{row.original.source === 'external' ? '外部接管' : '面板管理'}</Badge>
      {adoptionStatus === 'adopted' && scan?.certificate_id === row.original.id ? <Tooltip label="当前站点正在使用此证书"><Badge color="green" variant="light">使用中</Badge></Tooltip> : null}
    </Group> },
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
      title: cert.source === 'external' ? '取消接管' : '删除证书',
      message: cert.source === 'external'
        ? `确认取消「${cert.name}」的接管及元数据关联？不会删除外部证书或私钥，也不会修改站点配置。`
        : `确认删除证书「${cert.name}」及面板管理的文件？仍被站点引用的证书不能删除，请先替换相关站点证书。`,
      confirmLabel: cert.source === 'external' ? '确认取消接管' : '确认删除',
      errorTitle: cert.source === 'external' ? '取消接管失败' : '删除证书失败',
      onConfirm: async () => {
        await deleteMutation.mutateAsync(cert.id)
        notifySuccess({ message: cert.source === 'external' ? '已取消接管，原文件保留' : '证书已删除' })
        await refreshCertificateState()
      },
    })
  }

  return (
    <Stack gap="md">
      <Stack gap="sm" component="section" aria-label="当前站点证书">
        <Group justify="space-between" align="flex-start">
          <Group gap="xs">
            <Title order={4} size="h5">当前站点证书</Title>
            <Badge color={adoptionStatus === 'adopted' ? 'green' : adoptionStatus === 'changed' ? 'yellow' : 'gray'} variant="light">
              {scanQuery.isFetching ? '正在扫描' : adoptionStatus ? statusLabel[adoptionStatus] : '尚未扫描'}
            </Badge>
          </Group>
          <Button variant="light" disabled={busy} loading={scanQuery.isFetching} onClick={() => { void handleScan() }}>{scan ? '重新扫描' : '扫描当前站点'}</Button>
        </Group>
        <Text c="dimmed" size="sm">接管仅建立证书夹关联，不修改 Nginx 配置或重载服务。替换证书请使用下方证书夹中的“预览局部替换”。</Text>
        {scanQuery.isError ? <ErrorAlert error={scanQuery.error} title="扫描失败，请重新扫描后操作" /> : null}
        {scan ? <>
          {scan.read_only_reason ? <Alert color="yellow" title="当前配置仅支持只读展示">{scan.read_only_reason}</Alert> : null}
          {adoptionStatus === 'changed' ? <Alert color="yellow" title="接管信息待更新">当前配置或证书与上次接管记录不同，请核对扫描结果后更新接管信息。</Alert> : null}
          {scan.certificate ? <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="sm">
            <Stack gap={4}><Text c="dimmed">证书域名</Text><Text style={{ overflowWrap: 'anywhere' }}>{scan.certificate.dns_names.join(', ') || '-'}</Text></Stack>
            <Stack gap={4}><Text c="dimmed">签发者</Text><Text style={{ overflowWrap: 'anywhere' }}>{extractOrg(scan.certificate.issuer)}</Text></Stack>
            <Stack gap={4}><Text c="dimmed">到期时间</Text><TimeCell value={scan.certificate.not_after} /></Stack>
          </SimpleGrid> : null}
          {scan.cert_path ? <Stack gap={4}><Text c="dimmed">证书路径</Text><PathCell value={scan.cert_path} maxWidth="100%" wrap /></Stack> : null}
          {scan.key_path ? <Stack gap={4}><Text c="dimmed">私钥路径（不显示内容）</Text><PathCell value={scan.key_path} maxWidth="100%" wrap /></Stack> : null}
          <Group>
            <Button disabled={Boolean(scan.read_only_reason) || !scan.certificate || scanQuery.isFetching || scanQuery.isError || busy || adoptionStatus === 'adopted'} loading={adoptMutation.isPending} onClick={() => { void handleAdopt() }}>
              {adoptionStatus === 'adopted' ? '已接管' : adoptionStatus === 'changed' ? '更新接管信息' : '确认接管'}
            </Button>
            {adoptionStatus === 'adopted' ? <Text size="sm" c="dimmed" role="status">已关联到证书夹，原配置和文件保持不变。</Text> : null}
          </Group>
        </> : !scanQuery.isFetching && !scanQuery.isError ? <Text c="dimmed">扫描当前站点，查看实际使用的证书及接管状态。</Text> : null}
        {adoptMutation.isError ? <ErrorAlert error={adoptMutation.error} title="接管未能确认，请重新扫描" /> : null}
        {replaceMutation.isError && !replacement ? <Stack gap="xs"><ErrorAlert error={replaceMutation.error} title="替换未能确认" /><Text c="dimmed">请重新扫描实际证书引用，核对后确认接管。若要再次替换，请重新预览。</Text></Stack> : null}
      </Stack>
      <Divider my="xs" />
      <Group justify="space-between"><Title order={4} size="h5">证书夹</Title><Text c="dimmed" size="sm">上传新证书后预览引用差异，再确认应用。</Text></Group>
      {previewMutation.isError ? <ErrorAlert error={previewMutation.error} title="无法安全局部替换" /> : null}
      {certsQuery.isError ? <ErrorAlert error={certsQuery.error} title="加载证书夹失败" /> : null}
      <DataTable
        columns={columns}
        data={certsQuery.data || []}
        loading={certsQuery.isLoading || certsQuery.isFetching}
        emptyText="暂无证书，请先上传证书到证书夹"
        toolbarActions={<Button leftSection={<IconCloudUpload size={16} />} onClick={handlers.open}>上传到证书夹</Button>}
        renderRowActions={({ row }) => (
          <Group gap={4} wrap="nowrap">
            <Button size="compact-xs" variant="light" disabled={row.original.source === 'external' || busy || scanQuery.isFetching} loading={previewMutation.isPending && previewMutation.variables === row.original.id} onClick={() => { void handlePreview(row.original) }}>预览局部替换</Button>
            <Tooltip label={row.original.source === 'external' || site.is_imported || site.maintenance_enabled ? '请使用预览局部替换，不执行 marker 部署' : '部署到站点'}><ActionIcon aria-label="部署到站点" disabled={busy || row.original.source === 'external' || site.is_imported || site.maintenance_enabled || site.maintenance_pending} variant="subtle" loading={deployMutation.isPending} onClick={() => handleDeploy(row.original)}><IconRocket size={16} /></ActionIcon></Tooltip>
            <Tooltip label={row.original.source === 'external' ? '取消接管（保留文件）' : '删除'}><ActionIcon aria-label={row.original.source === 'external' ? '取消接管' : '删除证书'} color="red" variant="subtle" disabled={busy} loading={deleteMutation.isPending && deleteMutation.variables === row.original.id} onClick={() => handleDelete(row.original)}><IconTrash size={16} /></ActionIcon></Tooltip>
          </Group>
        )}
      />
      <Modal opened={Boolean(replacement)} onClose={() => { if (!replaceMutation.isPending) setReplacement(null) }} title="确认局部替换证书引用" size="lg" closeOnClickOutside={false} closeOnEscape={!replaceMutation.isPending} withCloseButton={!replaceMutation.isPending}>
        {replacement ? <Stack gap="md">
          <Alert color="blue">只修改目标 server 的证书与私钥路径引用。原外部文件、HTTPS 监听、维护块和业务配置保持不变；应用前校验哈希，执行 nginx -t 和 reload。</Alert>
          <Text size="sm" style={{ overflowWrap: 'anywhere' }}>已覆盖域名：{replacement.preview.matched_domains.join(', ')}</Text>
          {replacement.preview.unmatched_domains.length > 0 ? <Alert color="yellow" title="新证书仅覆盖部分绑定域名">
            <Text style={{ overflowWrap: 'anywhere' }}>未覆盖：{replacement.preview.unmatched_domains.join(', ')}</Text>
            <Text mt={4}>已覆盖至少一个绑定域名，可以继续应用；未覆盖域名访问 HTTPS 时可能出现证书错误。</Text>
          </Alert> : null}
          <Text size="sm">证书引用差异</Text>
          <PathCell value={replacement.preview.old_cert_path} maxWidth="100%" wrap />
          <Text size="sm">替换为</Text><PathCell value={replacement.preview.new_cert_path} maxWidth="100%" wrap />
          <Text size="sm">私钥引用差异（仅路径，不读取私钥内容）</Text>
          <PathCell value={replacement.preview.old_key_path} maxWidth="100%" wrap />
          <Text size="sm">替换为</Text><PathCell value={replacement.preview.new_key_path} maxWidth="100%" wrap />
          {replaceMutation.isError ? <Stack gap="xs">
            <ErrorAlert error={replaceMutation.error} title="替换未能确认" />
            <Text c="dimmed">请重新扫描实际证书引用并核对。当前预览不可重复提交；如需重试，请重新预览。</Text>
            <Button variant="light" onClick={() => { setReplacement(null); void handleScan() }}>重新扫描当前站点</Button>
          </Stack> : null}
          <Group justify="flex-end">
            <Button variant="default" disabled={replaceMutation.isPending} onClick={() => setReplacement(null)}>{replaceMutation.isError ? '关闭' : '取消'}</Button>
            <Button loading={replaceMutation.isPending} disabled={replaceMutation.isError} onClick={() => { void handleReplace() }}>确认应用</Button>
          </Group>
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
