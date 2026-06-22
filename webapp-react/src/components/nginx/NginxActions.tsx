import { Badge, Button, Checkbox, Grid, Stack, Text } from '@mantine/core'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { IconLink, IconRefresh, IconSearch, IconShieldCheck } from '@tabler/icons-react'
import { detectNginx, ensureInclude, reloadNginx, testNginx, type NginxDetectResponse, type NginxEnsureIncludeResponse, type NginxReloadResponse, type NginxTestResponse } from '@/api/nginx'
import { queryKeys } from '@/api/hooks'
import { MonoText } from '@/components/common/MonoText'
import { SectionCard } from '@/components/common/SectionCard'
import { confirmDanger } from '@/utils/confirm'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

function OutputBlock({ value }: { value?: string }) {
  if (!value) return null
  return <pre className="nginxOutputBlock">{value}</pre>
}

export function NginxActions() {
  const queryClient = useQueryClient()
  const [confirmModifyMainConf, setConfirmModifyMainConf] = useState(false)
  const [detectResult, setDetectResult] = useState<NginxDetectResponse | null>(null)
  const [ensureResult, setEnsureResult] = useState<NginxEnsureIncludeResponse | null>(null)
  const [testResult, setTestResult] = useState<NginxTestResponse | null>(null)
  const [reloadResult, setReloadResult] = useState<NginxReloadResponse | null>(null)
  const detectMutation = useMutation({ mutationFn: detectNginx })
  const ensureMutation = useMutation({ mutationFn: ensureInclude })
  const testMutation = useMutation({ mutationFn: testNginx })
  const reloadMutation = useMutation({ mutationFn: reloadNginx })

  async function refreshOverview() {
    await queryClient.invalidateQueries({ queryKey: queryKeys.systemOverview })
  }

  async function handleDetect() {
    try {
      const result = await detectMutation.mutateAsync()
      setDetectResult(result)
      notifySuccess({ message: result.test_ok ? 'Nginx 检测成功' : 'Nginx 检测完成但配置测试未通过' })
      await refreshOverview()
    } catch (error) {
      showErrorModal(error, '检测 Nginx 失败')
    }
  }

  function handleEnsureInclude() {
    confirmDanger({
      title: '安装 Include 入口',
      message: '安装面板 include 入口将修改 nginx.conf，在 http {} 块中插入 include 指令，使面板管理的站点配置生效。确认继续？',
      confirmLabel: '确认安装',
      errorTitle: '安装 Include 失败',
      onConfirm: async () => {
        const result = await ensureMutation.mutateAsync({ confirm_modify_main_conf: confirmModifyMainConf })
        setEnsureResult(result)
        notifySuccess({ message: result.changed ? 'Include 入口已安装' : 'Include 入口已存在，无需修改' })
        await refreshOverview()
      },
    })
  }

  async function handleTest() {
    try {
      const result = await testMutation.mutateAsync()
      setTestResult(result)
      notifySuccess({ message: result.ok ? '配置测试通过' : '配置测试未通过' })
    } catch (error) {
      showErrorModal(error, '配置测试失败')
    }
  }

  function handleReload() {
    confirmDanger({
      title: 'Reload Nginx',
      message: 'Reload 会先执行 nginx -t，再重新加载 Nginx 配置。如果配置异常可能影响线上服务。确认继续？',
      confirmLabel: '确认 Reload',
      errorTitle: 'Reload 失败',
      onConfirm: async () => {
        const result = await reloadMutation.mutateAsync({ test_before_reload: true })
        setReloadResult(result)
        notifySuccess({ message: 'Nginx Reload 成功' })
        await refreshOverview()
      },
    })
  }

  return (
    <Stack gap="sm">
      <Grid align="stretch">
        <Grid.Col span={{ base: 12, lg: 6 }}>
          <SectionCard h="100%" title="检测 Nginx" description="自动查找宿主机上的 Nginx，获取版本、prefix 和主配置路径。">
            <Stack gap="sm" align="flex-start">
              <Button className="nginxActionButton" leftSection={<IconSearch size={16} />} loading={detectMutation.isPending} onClick={handleDetect}>检测 Nginx</Button>
              {detectResult ? (
                <Grid w="100%" mt="xs">
                  <Grid.Col span={{ base: 12, md: 6 }}><Text size="sm" c="dimmed">Nginx 路径</Text><MonoText value={detectResult.bin} maxWidth="100%" /></Grid.Col>
                  <Grid.Col span={{ base: 12, md: 6 }}><Text size="sm" c="dimmed">版本</Text><Text>{detectResult.version || '-'}</Text></Grid.Col>
                  <Grid.Col span={{ base: 12, md: 6 }}><Text size="sm" c="dimmed">主配置</Text><MonoText value={detectResult.conf_path} maxWidth="100%" /></Grid.Col>
                  <Grid.Col span={{ base: 12, md: 6 }}><Text size="sm" c="dimmed">Prefix</Text><MonoText value={detectResult.prefix} maxWidth="100%" /></Grid.Col>
                  <Grid.Col span={12}><Badge color={detectResult.test_ok ? 'green' : 'red'} variant="light">配置测试 {detectResult.test_ok ? '通过' : '未通过'}</Badge><OutputBlock value={detectResult.stderr} /></Grid.Col>
                </Grid>
              ) : null}
            </Stack>
          </SectionCard>
        </Grid.Col>
        <Grid.Col span={{ base: 12, lg: 6 }}>
          <SectionCard h="100%" title="安装 Include 入口" description="将面板 include 指令写入 Nginx 主配置，使面板管理的站点配置生效。">
            <Stack gap="sm" align="flex-start">
              <Checkbox checked={confirmModifyMainConf} onChange={(event) => setConfirmModifyMainConf(event.currentTarget.checked)} label="确认允许修改 nginx.conf 主配置" />
              <Button className="nginxActionButton" color="yellow" leftSection={<IconLink size={16} />} loading={ensureMutation.isPending} onClick={handleEnsureInclude}>安装 Include</Button>
              {ensureResult ? <Stack gap={4} w="100%"><Badge color={ensureResult.installed ? 'green' : 'yellow'} variant="light">已安装 {ensureResult.installed ? '是' : '否'}</Badge><Text size="sm">是否有变更：{ensureResult.changed ? '是' : '否'}</Text><MonoText value={ensureResult.entry_file} maxWidth="100%" /><MonoText value={ensureResult.operation_id} maxWidth="100%" /></Stack> : null}
            </Stack>
          </SectionCard>
        </Grid.Col>
      </Grid>

      <Grid>
        <Grid.Col span={{ base: 12, md: 6 }}>
          <SectionCard title="配置测试" description="执行 nginx -t 检查当前配置是否正确。">
            <Button className="nginxActionButton" leftSection={<IconShieldCheck size={16} />} loading={testMutation.isPending} onClick={handleTest}>执行 nginx -t</Button>
            {testResult ? <Stack mt="md" gap="xs"><Badge color={testResult.ok ? 'green' : 'red'} variant="light">{testResult.ok ? '通过' : '未通过'}</Badge><OutputBlock value={testResult.stdout || testResult.stderr} /></Stack> : null}
          </SectionCard>
        </Grid.Col>
        <Grid.Col span={{ base: 12, md: 6 }}>
          <SectionCard title="Reload Nginx" description="重新加载 Nginx 配置，默认先自动执行 nginx -t。">
            <Button className="nginxActionButton" color="red" leftSection={<IconRefresh size={16} />} loading={reloadMutation.isPending} onClick={handleReload}>Reload Nginx</Button>
            {reloadResult ? <Stack mt="md" gap="xs"><Badge color={reloadResult.ok ? 'green' : 'red'} variant="light">{reloadResult.ok ? '成功' : '失败'}</Badge><MonoText value={reloadResult.operation_id} maxWidth="100%" /></Stack> : null}
          </SectionCard>
        </Grid.Col>
      </Grid>
    </Stack>
  )
}
