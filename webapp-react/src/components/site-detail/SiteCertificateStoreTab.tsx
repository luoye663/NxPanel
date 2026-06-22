import { ActionIcon, Button, Group, Modal, Stack, Textarea, TextInput, Tooltip } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { useForm } from '@mantine/form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconCloudUpload, IconRocket, IconTrash } from '@tabler/icons-react'
import type { MRT_ColumnDef } from 'mantine-react-table'
import { deleteCertificate, deployCertificate, listCertificates, uploadCertificate, type CertificateItem } from '@/api/certificates'
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
  const form = useForm<UploadFormValues>({
    initialValues: { name: '', certificate_pem: '', private_key_pem: '' },
    validate: {
      name: (value) => value.trim() ? null : '请输入证书名称',
      certificate_pem: (value) => value.trim() ? null : '请输入证书内容',
      private_key_pem: (value) => value.trim() ? null : '请输入私钥内容',
    },
  })

  const columns: MRT_ColumnDef<CertificateItem>[] = [
    { accessorKey: 'domains', header: '域名', size: 220, Cell: ({ row }) => <MonoText value={row.original.domains?.join(', ') || '-'} maxWidth={260} /> },
    { accessorKey: 'not_after', header: '到期时间', size: 180, Cell: ({ row }) => <TimeCell value={row.original.not_after} /> },
    { accessorKey: 'issuer', header: '证书品牌', size: 150, Cell: ({ row }) => extractOrg(row.original.issuer) },
    { accessorKey: 'cert_path', header: '证书位置', Cell: ({ row }) => <PathCell value={row.original.cert_path} maxWidth={260} /> },
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
      message: `确认删除证书「${cert.name}」？已部署该证书的站点将不受影响，但证书将从证书夹中移除。`,
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
      {certsQuery.isError ? <ErrorAlert error={certsQuery.error} title="加载证书夹失败" /> : null}
      <DataTable
        columns={columns}
        data={certsQuery.data || []}
        loading={certsQuery.isLoading || certsQuery.isFetching}
        emptyText="暂无证书，请先上传证书到证书夹"
        toolbarActions={<Button leftSection={<IconCloudUpload size={16} />} onClick={handlers.open}>上传到证书夹</Button>}
        renderRowActions={({ row }) => (
          <Group gap={4} wrap="nowrap">
            <Tooltip label="部署到站点"><ActionIcon variant="subtle" loading={deployMutation.isPending} onClick={() => handleDeploy(row.original)}><IconRocket size={16} /></ActionIcon></Tooltip>
            <Tooltip label="删除"><ActionIcon color="red" variant="subtle" loading={deleteMutation.isPending} onClick={() => handleDelete(row.original)}><IconTrash size={16} /></ActionIcon></Tooltip>
          </Group>
        )}
      />
      <Modal opened={opened} onClose={handlers.close} title="上传证书到证书夹" size="lg" closeOnClickOutside={false} centered>
        <form onSubmit={form.onSubmit(handleUpload)}>
          <Stack gap="md">
            <TextInput label="证书名称" placeholder="例如: example.com" {...form.getInputProps('name')} />
            <Textarea label="证书内容 (PEM)" minRows={5} autosize placeholder="-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----" {...form.getInputProps('certificate_pem')} />
            <Textarea label="私钥内容 (PEM)" minRows={5} autosize placeholder="-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----" {...form.getInputProps('private_key_pem')} />
            {/* 证书私钥只提交给后端保存，前端不做日志输出或本地持久化。 */}
            <Group justify="flex-end"><Button variant="default" onClick={handlers.close}>取消</Button><Button type="submit" loading={uploadMutation.isPending}>上传</Button></Group>
          </Stack>
        </form>
      </Modal>
    </Stack>
  )
}
