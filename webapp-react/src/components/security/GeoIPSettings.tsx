import { Alert, Button, FileInput, Group, LoadingOverlay, Stack, Switch, Text, TextInput, Textarea } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconDatabase, IconDeviceFloppy, IconRefresh, IconUpload } from '@tabler/icons-react'
import { useEffect, useState } from 'react'
import { geoAccessKeys, getGeoIPSettings, getGeoIPStatus, updateGeoIPDatabase, updateGeoIPSettings, uploadGeoIPDatabase } from '@/api/geoAccess'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { SectionCard } from '@/components/common/SectionCard'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

interface FormValues { accountId: string; licenseKey: string; autoUpdate: boolean; trustedProxies: string }

export function GeoIPSettingsPanel() {
  const queryClient = useQueryClient()
  const [databaseFile, setDatabaseFile] = useState<File | null>(null)
  const settingsQuery = useQuery({ queryKey: geoAccessKeys.settings, queryFn: getGeoIPSettings })
  const statusQuery = useQuery({ queryKey: geoAccessKeys.status, queryFn: getGeoIPStatus })
  const form = useForm<FormValues>({ initialValues: { accountId: '', licenseKey: '', autoUpdate: true, trustedProxies: '' } })

  useEffect(() => {
    if (!settingsQuery.data) return
    form.setValues({ accountId: settingsQuery.data.account_id, licenseKey: '', autoUpdate: settingsQuery.data.auto_update, trustedProxies: settingsQuery.data.trusted_proxies.join('\n') })
    form.resetDirty()
  }, [settingsQuery.data])

  const saveMutation = useMutation({
    mutationFn: (values: FormValues) => updateGeoIPSettings({
      account_id: values.accountId.trim(),
      ...(values.licenseKey.trim() ? { license_key: values.licenseKey.trim() } : {}),
      auto_update: values.autoUpdate,
      trusted_proxies: values.trustedProxies.split(/[\n,]/).map((value) => value.trim()).filter(Boolean),
    }),
    onSuccess: async () => { notifySuccess({ message: 'GeoIP 设置已保存' }); await queryClient.invalidateQueries({ queryKey: geoAccessKeys.settings }) },
    onError: (error) => showErrorModal(error, '保存 GeoIP 设置失败'),
  })
  const updateMutation = useMutation({
    mutationFn: updateGeoIPDatabase,
    onSuccess: async () => { notifySuccess({ message: 'GeoLite2 数据库已更新' }); await queryClient.invalidateQueries({ queryKey: geoAccessKeys.status }) },
    onError: (error) => showErrorModal(error, '更新 GeoLite2 数据库失败'),
  })
  const uploadMutation = useMutation({
    mutationFn: uploadGeoIPDatabase,
    onSuccess: async () => { setDatabaseFile(null); notifySuccess({ message: 'GeoLite2 数据库已安装' }); await queryClient.invalidateQueries({ queryKey: geoAccessKeys.status }) },
    onError: (error) => showErrorModal(error, '上传 GeoLite2 数据库失败'),
  })

  return (
    <Stack gap="md">
      {(settingsQuery.isError || statusQuery.isError) ? <ErrorAlert error={settingsQuery.error || statusQuery.error} title="加载 GeoIP 配置失败" /> : null}
      <SectionCard title="GeoLite2 Country 数据库" pos="relative">
        <LoadingOverlay visible={statusQuery.isLoading} />
        <Stack gap="md">
          <Group gap="xs"><IconDatabase size={18} /><Text fw={500}>{statusQuery.data?.installed ? '已安装' : '未安装'}</Text></Group>
          {statusQuery.data?.last_error ? <Alert color="red">{statusQuery.data.last_error}</Alert> : null}
          {statusQuery.data?.installed ? <Text size="sm" c="dimmed">国家/地区 {statusQuery.data.countries.length} 个，启用站点 {statusQuery.data.enabled_sites} 个，构建时间 {new Date(statusQuery.data.build_epoch * 1000).toLocaleString()}</Text> : null}
          <Group align="end">
            <FileInput flex={1} label="手动上传 MMDB" placeholder="选择 GeoLite2-Country.mmdb" accept=".mmdb,application/octet-stream" value={databaseFile} onChange={setDatabaseFile} leftSection={<IconUpload size={16} />} />
            <Button variant="default" disabled={!databaseFile} loading={uploadMutation.isPending} onClick={() => databaseFile && uploadMutation.mutate(databaseFile)}>上传</Button>
            <Button leftSection={<IconRefresh size={16} />} loading={updateMutation.isPending} onClick={() => updateMutation.mutate()}>在线更新</Button>
          </Group>
        </Stack>
      </SectionCard>

      <SectionCard title="更新与代理" pos="relative">
        <LoadingOverlay visible={settingsQuery.isLoading} />
        <form onSubmit={form.onSubmit((values) => saveMutation.mutate(values))}>
          <Stack gap="md">
            <TextInput label="MaxMind Account ID" {...form.getInputProps('accountId')} />
            <TextInput type="password" label="License Key" placeholder={settingsQuery.data?.license_key_masked || '输入新的 License Key'} description="留空会保留当前密钥。密钥在服务器端加密存储。" {...form.getInputProps('licenseKey')} />
            <Switch label="每日自动更新" {...form.getInputProps('autoUpdate', { type: 'checkbox' })} />
            <Textarea label="可信代理 IP / CIDR" minRows={4} placeholder={'127.0.0.1\n10.0.0.0/8'} description="仅在请求确实经过这些代理时读取 X-Forwarded-For；直连请求始终使用连接源 IP。" {...form.getInputProps('trustedProxies')} />
            <Group justify="flex-end"><Button type="submit" leftSection={<IconDeviceFloppy size={16} />} loading={saveMutation.isPending}>保存</Button></Group>
          </Stack>
        </form>
      </SectionCard>
    </Stack>
  )
}
