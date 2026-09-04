import { Alert, Badge, Button, Checkbox, Code, FileButton, Group, Loader, Modal, Paper, Select, SimpleGrid, Stack, Tabs, Text, ThemeIcon, Title } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { IconAlertTriangle, IconBox, IconCheck, IconCode, IconDownload, IconPlugConnected, IconRefresh, IconShieldCheck, IconTrash, IconUpload } from '@tabler/icons-react'
import { useEffect, useMemo, useState } from 'react'
import { getPluginInstallAuthorizationIssue, type DeveloperPackageInspection, type PluginAuthorizationSummary, type PluginCatalogItem, type PluginEntitlementDeniedReason, type PluginInstallation, type PluginPermission } from '@/api/plugins'
import { useInspectDeveloperPackage, useInstallDeveloperPackage, useInstallPlugin, useInstalledPlugins, usePluginCatalog, usePluginDeveloperMode, usePluginRepositoryStatus, useRefreshPluginCatalog, useSetPluginDeveloperMode, useTogglePlugin, useUninstallPlugin, useUpdatePlugin } from '@/api/pluginHooks'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { PageHeader } from '@/components/common/PageHeader'
import { PageShell } from '@/components/common/PageShell'
import { PluginAuthorizationModal, type PendingAuthorizedInstall } from '@/components/plugins/PluginAuthorizationModal'
import { confirmDanger } from '@/utils/confirm'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

type InstalledFilter = 'all' | 'official' | 'developer' | 'updates'

function healthColor(health?: string) {
  if (health === 'healthy') return 'green'
  if (health === 'degraded') return 'yellow'
  if (health === 'unhealthy') return 'red'
  return 'gray'
}

function PermissionList({ permissions }: { permissions: PluginPermission[] }) {
  if (!permissions.length) return <Text c="dimmed">未申请额外能力。</Text>
  return <Stack gap="xs">{permissions.map((permission) => <Group key={permission.name} align="flex-start" wrap="nowrap"><ThemeIcon variant="light" color={permission.risk === 'high' ? 'red' : permission.risk === 'medium' ? 'yellow' : 'blue'} size="sm"><IconCheck size={13} /></ThemeIcon><div><Text fw={600}>{permission.label || permission.name}</Text>{permission.description ? <Text size="xs" c="dimmed">{permission.description}</Text> : null}</div></Group>)}</Stack>
}

function PermissionApprovalModal({ plugin, update, opened, onClose, onApprove, loading }: { plugin: PluginCatalogItem | null; update: boolean; opened: boolean; onClose: () => void; onApprove: (permissions: string[]) => void; loading: boolean }) {
  const [accepted, setAccepted] = useState(false)
  const permissions = plugin?.permissions || []
  useEffect(() => { if (opened) setAccepted(false) }, [opened, plugin?.id, plugin?.version])
  return <Modal opened={opened} onClose={onClose} title={update ? '确认插件更新权限' : '确认插件权限'} size="lg" closeOnClickOutside={false}><Stack><Text>「{plugin?.name}」来自 nxPanel 官方仓库，将获得以下明确授权。</Text><Paper withBorder p="sm"><PermissionList permissions={permissions} /></Paper>{permissions.some((item) => item.risk === 'high') ? <Alert color="orange" icon={<IconAlertTriangle size={18} />} title="包含高风险能力">请确认能力用途符合预期。后续更新如新增权限，系统会再次要求确认。</Alert> : null}<Checkbox checked={accepted} onChange={(event) => setAccepted(event.currentTarget.checked)} label="我已阅读并同意授予上述权限" /><Group justify="flex-end"><Button variant="default" onClick={onClose}>取消</Button><Button loading={loading} disabled={!accepted} onClick={() => onApprove(permissions.map((item) => item.name))}>{update ? '授权并更新' : '授权并安装'}</Button></Group></Stack></Modal>
}

function PluginIdentity({ plugin, developer = false }: { plugin: PluginCatalogItem; developer?: boolean }) {
  return <Group align="flex-start" wrap="nowrap"><ThemeIcon size={38} variant="light" color={developer ? 'orange' : 'blue'}>{developer ? <IconCode size={22} /> : <IconBox size={22} />}</ThemeIcon><div className="pluginIdentityCopy"><Title order={3} size="h5">{plugin.name}</Title><Text size="xs" c="dimmed">{plugin.publisher} · {plugin.version}</Text></div></Group>
}

function OfficialPluginCard({ plugin, installation, onInstall, onUpdate, onToggle, onUninstall, busy }: { plugin: PluginCatalogItem; installation?: PluginInstallation; onInstall: () => void; onUpdate: () => void; onToggle: () => void; onUninstall: () => void; busy: boolean }) {
  const installed = Boolean(installation || plugin.installed_version)
  const enabled = installation?.enabled || plugin.state === 'enabled'
  return <Paper withBorder p="md" radius="md" className="pluginCatalogItem"><Stack h="100%" justify="space-between"><Stack gap="sm"><Group justify="space-between" align="flex-start" wrap="nowrap"><PluginIdentity plugin={plugin} /><Group gap={6} justify="flex-end"><Badge variant="light" color="blue" leftSection={<IconShieldCheck size={12} />}>官方验证</Badge>{plugin.access === 'licensed' ? <Badge variant="outline" color="gray">需授权</Badge> : null}</Group></Group><Text>{plugin.summary}</Text>{plugin.required_providers?.length ? <Text size="xs" c="dimmed">受控原生能力：{plugin.required_providers.join('、')}</Text> : null}{!plugin.compatible ? <Alert color="yellow" title="当前环境不兼容">{plugin.incompatibility_reason || '此版本不支持当前 nxPanel 或 Nginx 运行环境。'}</Alert> : null}{installation?.last_error ? <Alert color="red" title="最近错误">{installation.last_error}</Alert> : null}</Stack><Group mt="md" justify="space-between"><Group gap="xs">{installed ? <Badge color={enabled ? 'green' : 'gray'}>{enabled ? '已启用' : '已停用'}</Badge> : null}{installation?.health ? <Badge variant="light" color={healthColor(installation.health)}>{installation.health}</Badge> : null}</Group><Group gap="xs">{installed ? <><Button variant="default" loading={busy} onClick={onToggle}>{enabled ? '停用' : '启用'}</Button>{plugin.update_available ? <Button leftSection={<IconDownload size={15} />} loading={busy} onClick={onUpdate}>更新</Button> : null}<Button variant="subtle" color="red" aria-label={`卸载 ${plugin.name}`} title={enabled ? '请先停用插件再卸载' : undefined} disabled={enabled} onClick={onUninstall}><IconTrash size={16} /></Button></> : <Button leftSection={<IconDownload size={15} />} disabled={!plugin.compatible} loading={busy} onClick={onInstall}>安装</Button>}</Group></Group></Stack></Paper>
}

function InstalledPluginCard({ plugin, developerMode, onToggle, onUninstall, busy }: { plugin: PluginInstallation; developerMode: boolean; onToggle: () => void; onUninstall: () => void; busy: boolean }) {
  const developer = plugin.source === 'developer' || plugin.verification_status === 'developer_unverified'
  const cannotEnable = developer && !developerMode && !plugin.enabled
  return <Paper withBorder p="md" radius="md" className="pluginInstalledRow"><Stack gap="sm"><Group justify="space-between" align="flex-start" wrap="nowrap" className="pluginInstalledHeader"><PluginIdentity plugin={plugin} developer={developer} /><Group gap={6} justify="flex-end"><Badge color={plugin.enabled ? 'green' : 'gray'}>{plugin.enabled ? '已启用' : '已停用'}</Badge><Badge variant="light" color={developer ? 'orange' : 'blue'}>{developer ? '开发者安装' : '官方来源'}</Badge><Badge variant="outline" color={developer ? 'orange' : 'green'}>{developer ? '未验证身份' : 'TUF 已验证'}</Badge></Group></Group><Text>{plugin.summary}</Text>{plugin.source_conflict ? <Alert color="red" title="插件 ID 与官方目录冲突">移除此开发者插件后，才能安装同 ID 的官方插件。当前插件不能更新或重新启用。</Alert> : null}{plugin.last_error ? <Alert color="red" title="最近错误">{plugin.last_error}</Alert> : null}<Group justify="space-between"><Group gap="xs"><Badge variant="light" color={healthColor(plugin.health)}>健康状态：{plugin.health || 'unknown'}</Badge>{plugin.required_providers?.map((provider) => <Badge key={provider} variant="outline" color="gray">{provider}</Badge>)}</Group><Group gap="xs"><Button variant="default" loading={busy} disabled={cannotEnable || (Boolean(plugin.source_conflict) && !plugin.enabled)} title={cannotEnable ? '开启开发者模式后才能重新启用' : undefined} onClick={onToggle}>{plugin.enabled ? '停用' : '启用'}</Button><Button variant="subtle" color="red" aria-label={`卸载 ${plugin.name}`} disabled={plugin.enabled} title={plugin.enabled ? '请先停用插件再卸载' : undefined} onClick={onUninstall}><IconTrash size={16} /></Button></Group></Group></Stack></Paper>
}

function DeveloperReview({ inspection, accepted, onAccepted, onInstall, installing }: { inspection: DeveloperPackageInspection; accepted: boolean; onAccepted: (accepted: boolean) => void; onInstall: () => void; installing: boolean }) {
  const conflicts = inspection.conflicts || []
  return <Paper withBorder p="md" radius="md"><Stack><div><Title order={3} size="h5">安装前复核</Title><Text c="dimmed">包已通过结构、资源预算和文件摘要检查；发布者身份仍未验证。</Text></div><Alert color="orange" icon={<IconAlertTriangle size={18} />} title="开发者包未经过 nxPanel 官方签名验证">“{inspection.publisher}”是包内自行声明的发布者，不能证明真实来源。只安装你亲自构建或从可信渠道获得的文件。</Alert><dl className="pluginReviewGrid"><div><dt>文件</dt><dd>{inspection.filename}</dd></div><div><dt>插件 ID</dt><dd><Code>{inspection.id}</Code></dd></div><div><dt>名称 / 版本</dt><dd>{inspection.name} · {inspection.version}</dd></div><div><dt>声明发布者</dt><dd>{inspection.publisher}</dd></div><div className="pluginReviewWide"><dt>SHA-256</dt><dd><Code className="pluginDigest">{inspection.package_sha256}</Code></dd></div><div className="pluginReviewWide"><dt>网络域名</dt><dd>{inspection.network_domains?.length ? inspection.network_domains.join('、') : '无'}</dd></div><div className="pluginReviewWide"><dt>受控 Provider</dt><dd>{inspection.required_providers?.length ? inspection.required_providers.join('、') : '无'}</dd></div></dl><Paper withBorder p="sm"><Text fw={600} mb="xs">申请权限</Text><PermissionList permissions={inspection.permissions || []} /></Paper>{conflicts.length ? <Alert color="red" title="不能安装">{conflicts.join('；')}</Alert> : null}<Checkbox checked={accepted} onChange={(event) => onAccepted(event.currentTarget.checked)} label="我了解此包身份未验证，并确认授予上列全部权限" /><Group justify="flex-end"><Button loading={installing} disabled={!accepted || conflicts.length > 0} color="orange" onClick={onInstall}>确认安装本地包</Button></Group></Stack></Paper>
}

export function PluginCenterPage() {
  const catalogQuery = usePluginCatalog(); const installedQuery = useInstalledPlugins(); const repositoryQuery = usePluginRepositoryStatus(); const developerModeQuery = usePluginDeveloperMode()
  const refreshMutation = useRefreshPluginCatalog(); const installMutation = useInstallPlugin(); const updateMutation = useUpdatePlugin(); const toggleMutation = useTogglePlugin(); const uninstallMutation = useUninstallPlugin(); const setDeveloperModeMutation = useSetPluginDeveloperMode(); const inspectMutation = useInspectDeveloperPackage(); const installDeveloperMutation = useInstallDeveloperPackage()
  const [activeTab, setActiveTab] = useState<string | null>('discover'); const [installedFilter, setInstalledFilter] = useState<InstalledFilter>('all'); const [approvalPlugin, setApprovalPlugin] = useState<PluginCatalogItem | null>(null); const [approvalUpdate, setApprovalUpdate] = useState(false); const [inspection, setInspection] = useState<DeveloperPackageInspection | null>(null); const [developerPackageAccepted, setDeveloperPackageAccepted] = useState(false); const [developerRiskAccepted, setDeveloperRiskAccepted] = useState(false)
  const [authorizationRequest, setAuthorizationRequest] = useState<PendingAuthorizedInstall | null>(null); const [suggestedAuthorizations, setSuggestedAuthorizations] = useState<PluginAuthorizationSummary[]>([]); const [deniedReason, setDeniedReason] = useState<PluginEntitlementDeniedReason | undefined>()
  const [approvalOpened, approvalHandlers] = useDisclosure(false); const [developerModeModalOpened, developerModeModalHandlers] = useDisclosure(false); const [authorizationOpened, authorizationHandlers] = useDisclosure(false)
  const installedById = useMemo(() => new Map((installedQuery.data || []).map((item) => [item.id, item])), [installedQuery.data])
  const installedItems = useMemo(() => (installedQuery.data || []).filter((item) => { const developer = item.source === 'developer' || item.verification_status === 'developer_unverified'; if (installedFilter === 'official') return !developer; if (installedFilter === 'developer') return developer; if (installedFilter === 'updates') return Boolean(item.update_available); return true }), [installedFilter, installedQuery.data])
  const developerMode = Boolean(developerModeQuery.data?.enabled)
  const runningDeveloperPlugins = developerModeQuery.data?.running_developer_plugins ?? (installedQuery.data || []).filter((item) => item.enabled && (item.source === 'developer' || item.verification_status === 'developer_unverified')).length
  const busy = installMutation.isPending || updateMutation.isPending || toggleMutation.isPending || uninstallMutation.isPending
  function openApproval(plugin: PluginCatalogItem, update = false) { setApprovalPlugin(plugin); setApprovalUpdate(update); approvalHandlers.open() }
  function openAuthorization(request: PendingAuthorizedInstall, error: unknown) {
    const issue = getPluginInstallAuthorizationIssue(error)
    if (!issue) return false
    setAuthorizationRequest(request)
    setSuggestedAuthorizations(issue.details.authorizations || [])
    setDeniedReason(issue.kind === 'entitlement_denied' ? issue.details.reason : undefined)
    approvalHandlers.close()
    authorizationHandlers.open()
    return true
  }
  async function submitOfficial(request: PendingAuthorizedInstall) {
    if (request.update) await updateMutation.mutateAsync({ plugin: request.plugin, permissions: request.permissions })
    else await installMutation.mutateAsync({ plugin: request.plugin, permissions: request.permissions })
  }
  async function approve(permissions: string[]) {
    if (!approvalPlugin) return
    const request = { plugin: approvalPlugin, permissions, update: approvalUpdate }
    try {
      await submitOfficial(request)
      approvalHandlers.close()
      notifySuccess({ message: approvalUpdate ? '插件更新已提交' : '插件安装已提交' })
    }
    catch (error) {
      if (!openAuthorization(request, error)) showErrorModal(error, approvalUpdate ? '更新插件失败' : '安装插件失败')
    }
  }
  async function continueAuthorizedInstall() {
    if (!authorizationRequest) return
    try {
      await submitOfficial(authorizationRequest)
      authorizationHandlers.close()
      setAuthorizationRequest(null)
      setDeniedReason(undefined)
      notifySuccess({ message: authorizationRequest.update ? '账户权益已确认，插件更新已提交' : '账户权益已确认，插件安装已提交' })
    }
    catch (error) {
      const issue = getPluginInstallAuthorizationIssue(error)
      if (issue) {
        setSuggestedAuthorizations(issue.details.authorizations || [])
        setDeniedReason(issue.kind === 'entitlement_denied' ? issue.details.reason : undefined)
        return
      }
      throw error
    }
  }
  async function refresh() { try { await refreshMutation.mutateAsync(); notifySuccess({ message: '官方插件目录已刷新' }) } catch (error) { showErrorModal(error, '刷新官方插件目录失败') } }
  async function setDeveloperMode(enabled: boolean) { try { await setDeveloperModeMutation.mutateAsync({ enabled, acknowledgeRisk: enabled }); developerModeModalHandlers.close(); setDeveloperRiskAccepted(false); notifySuccess({ message: enabled ? '开发者模式已开启' : '开发者模式已关闭' }) } catch (error) { showErrorModal(error, enabled ? '开启开发者模式失败' : '关闭开发者模式失败') } }
  async function inspectPackage(file: File | null) { if (!file) return; setInspection(null); setDeveloperPackageAccepted(false); if (!file.name.toLowerCase().endsWith('.nxp')) { showErrorModal(new Error('请选择扩展名为 .nxp 的本地插件包。'), '文件格式不正确'); return } try { setInspection(await inspectMutation.mutateAsync(file)) } catch (error) { showErrorModal(error, '检查插件包失败') } }
  async function installDeveloper() { if (!inspection) return; try { await installDeveloperMutation.mutateAsync({ uploadToken: inspection.upload_token, approvedPermissions: (inspection.permissions || []).map((item) => item.name) }); setInspection(null); setDeveloperPackageAccepted(false); notifySuccess({ message: '本地插件已安装，启用前请再次确认运行状态' }); setActiveTab('installed'); setInstalledFilter('developer') } catch (error) { showErrorModal(error, '安装本地插件失败') } }
  function uninstall(plugin: PluginInstallation | PluginCatalogItem) { confirmDanger({ title: '卸载插件', message: `确认卸载「${plugin.name}」？插件数据默认保留，可在重新安装后恢复。`, confirmLabel: '卸载', onConfirm: async () => { await uninstallMutation.mutateAsync({ pluginId: plugin.id }); notifySuccess({ message: '插件已卸载，数据已保留' }) } }) }

  return <PageShell><PageHeader title="" subtitle="官方扩展统一来自 nxPanel 内置可信仓库；本地包仅在开发者模式下开放。" actions={activeTab === 'discover' ? <Button variant="default" leftSection={<IconRefresh size={16} />} loading={refreshMutation.isPending} onClick={refresh}>刷新官方目录</Button> : undefined} />
    {developerMode ? <Alert color="orange" icon={<IconAlertTriangle size={18} />} title="开发者模式已开启">可以安装未经官方身份验证的本地插件包。沙箱和权限检查仍然生效，但你必须自行确认代码来源。</Alert> : null}
    {!developerMode && runningDeveloperPlugins > 0 ? <Alert color="orange" icon={<IconAlertTriangle size={18} />} title={`开发者模式已关闭，但仍有 ${runningDeveloperPlugins} 个开发者插件正在运行`}>它们不会被自动停用。你仍可停用或卸载，但重新启用、上传和本地更新前必须再次开启开发者模式。</Alert> : null}
    <Tabs value={activeTab} onChange={setActiveTab} keepMounted={false}><Tabs.List aria-label="插件中心分类"><Tabs.Tab value="discover" leftSection={<IconPlugConnected size={15} />}>发现</Tabs.Tab><Tabs.Tab value="installed" leftSection={<IconBox size={15} />}>已安装</Tabs.Tab><Tabs.Tab value="developer" leftSection={<IconCode size={15} />}>开发者</Tabs.Tab></Tabs.List>
      <Tabs.Panel value="discover"><Stack><Alert color="blue" icon={<IconShieldCheck size={18} />}>此处只展示 nxPanel 官方接口发布、经内置 TUF 信任根验证的插件。特权操作只能调用固定且可审计的 Agent Provider。</Alert>{repositoryQuery.isLoading ? <Group justify="center" py="lg"><Loader size="sm" /></Group> : null}{repositoryQuery.isError ? <ErrorAlert error={repositoryQuery.error} title="读取官方仓库状态失败" /> : null}{repositoryQuery.data && !repositoryQuery.data.configured ? <Alert color="yellow" title="此构建未配置官方插件仓库">正式发行版需要在构建时注入官方仓库地址和 TUF 根。当前不能安装官方插件。</Alert> : null}{repositoryQuery.data?.using_cached_metadata ? <Alert color="yellow" title="正在使用可信缓存">官方仓库暂时不可达。缓存过期后会停止新的安装和更新，已安装插件不受影响。</Alert> : null}{repositoryQuery.data?.last_error ? <Alert color="red" title="最近一次目录刷新失败">{repositoryQuery.data.last_error}</Alert> : null}{catalogQuery.isLoading || installedQuery.isLoading ? <Group justify="center" py="xl"><Loader /></Group> : null}{catalogQuery.isError ? <ErrorAlert error={catalogQuery.error} title="加载官方插件目录失败" /> : null}{installedQuery.isError ? <ErrorAlert error={installedQuery.error} title="加载安装状态失败" /> : null}{!catalogQuery.isLoading && !catalogQuery.isError && (catalogQuery.data || []).length === 0 ? <Alert color="gray" title="官方目录中暂无插件">稍后刷新目录。开发者自装包不会出现在“发现”中。</Alert> : null}<SimpleGrid cols={{ base: 1, lg: 2 }}>{(catalogQuery.data || []).map((plugin) => { const installation = installedById.get(plugin.id); return <OfficialPluginCard key={plugin.id} plugin={plugin} installation={installation} busy={busy} onInstall={() => openApproval(plugin)} onUpdate={() => openApproval(plugin, true)} onToggle={async () => { try { await toggleMutation.mutateAsync({ pluginId: plugin.id, enabled: !installation?.enabled }); notifySuccess({ message: installation?.enabled ? '插件已停用' : '插件已启用' }) } catch (error) { showErrorModal(error, installation?.enabled ? '停用插件失败' : '启用插件失败') } }} onUninstall={() => uninstall(plugin)} /> })}</SimpleGrid></Stack></Tabs.Panel>
      <Tabs.Panel value="installed"><Stack><Group justify="space-between" className="pluginInstalledToolbar"><Select aria-label="筛选已安装插件" value={installedFilter} onChange={(value) => setInstalledFilter((value || 'all') as InstalledFilter)} data={[{ value: 'all', label: '全部来源' }, { value: 'official', label: '官方来源' }, { value: 'developer', label: '开发者安装' }, { value: 'updates', label: '可更新' }]} /><Text c="dimmed">{installedItems.length} 个插件</Text></Group>{installedQuery.isLoading ? <Group justify="center" py="xl"><Loader /></Group> : null}{installedQuery.isError ? <ErrorAlert error={installedQuery.error} title="加载已安装插件失败" /> : null}{!installedQuery.isLoading && !installedQuery.isError && installedItems.length === 0 ? <Alert color="gray" title="没有匹配的已安装插件">从“发现”安装官方插件，或在“开发者”中检查本地包。</Alert> : null}<Stack>{installedItems.map((plugin) => <InstalledPluginCard key={plugin.id} plugin={plugin} developerMode={developerMode} busy={busy} onToggle={async () => { try { await toggleMutation.mutateAsync({ pluginId: plugin.id, enabled: !plugin.enabled }); notifySuccess({ message: plugin.enabled ? '插件已停用' : '插件已启用' }) } catch (error) { showErrorModal(error, plugin.enabled ? '停用插件失败' : '启用插件失败') } }} onUninstall={() => uninstall(plugin)} />)}</Stack></Stack></Tabs.Panel>
      <Tabs.Panel value="developer"><Stack>{developerModeQuery.isLoading ? <Group justify="center" py="lg"><Loader size="sm" /></Group> : null}{developerModeQuery.isError ? <ErrorAlert error={developerModeQuery.error} title="读取开发者模式失败" /> : null}<Paper withBorder p="md" radius="md"><Group justify="space-between" align="flex-start" className="pluginDeveloperModeRow"><div><Group gap="xs"><Title order={3} size="h5">开发者模式</Title><Badge color={developerMode ? 'orange' : 'gray'}>{developerMode ? '已开启' : '已关闭'}</Badge></Group><Text c="dimmed" mt={4}>只允许从当前设备选择本地 `.nxp` 文件；不支持 URL 安装或第三方插件仓库。</Text></div>{developerMode ? <Button variant="default" loading={setDeveloperModeMutation.isPending} onClick={() => setDeveloperMode(false)}>关闭开发者模式</Button> : <Button color="orange" onClick={developerModeModalHandlers.open}>开启开发者模式</Button>}</Group></Paper>{developerMode ? <Paper withBorder p="md" radius="md"><Stack><div><Title order={3} size="h5">检查本地插件包</Title><Text c="dimmed">选择文件后只做检查，不会立即安装。检查结果 15 分钟内有效，API 重启后失效。</Text></div><Group><FileButton onChange={inspectPackage} accept=".nxp,application/gzip,application/x-gzip">{(props) => <Button {...props} variant="default" leftSection={<IconUpload size={16} />} loading={inspectMutation.isPending}>选择 .nxp 文件</Button>}</FileButton><Text size="xs" c="dimmed">上限 64 MiB；展开后上限 256 MiB</Text></Group>{inspectMutation.isError ? <ErrorAlert error={inspectMutation.error} title="检查插件包失败" /> : null}</Stack></Paper> : <Alert color="gray" title="本地安装入口已锁定">开启开发者模式并确认风险后，才能选择和检查本地 `.nxp` 文件。</Alert>}{inspection ? <DeveloperReview inspection={inspection} accepted={developerPackageAccepted} onAccepted={setDeveloperPackageAccepted} onInstall={installDeveloper} installing={installDeveloperMutation.isPending} /> : null}</Stack></Tabs.Panel>
    </Tabs><PermissionApprovalModal plugin={approvalPlugin} update={approvalUpdate} opened={approvalOpened} onClose={approvalHandlers.close} onApprove={approve} loading={installMutation.isPending || updateMutation.isPending} /><PluginAuthorizationModal opened={authorizationOpened} request={authorizationRequest} suggestedAuthorizations={suggestedAuthorizations} deniedReason={deniedReason} onClose={() => { authorizationHandlers.close(); setAuthorizationRequest(null); setDeniedReason(undefined) }} onAuthorized={continueAuthorizedInstall} /><Modal opened={developerModeModalOpened} onClose={developerModeModalHandlers.close} title="开启开发者模式" size="lg" closeOnClickOutside={false}><Stack><Alert color="orange" icon={<IconAlertTriangle size={18} />} title="这是高风险功能">本地包没有官方 TUF 签名，包内发布者名称也不能证明身份。插件仍受 WASM 沙箱、权限、配额和固定 Provider 边界限制，但获批能力可读取或修改面板数据。</Alert><Text>开发者模式是全局设置。关闭它不会自动停用已安装或正在运行的开发者插件，只会禁止新的上传、本地更新和重新启用。</Text><Checkbox checked={developerRiskAccepted} onChange={(event) => setDeveloperRiskAccepted(event.currentTarget.checked)} label="我理解本地插件身份未经验证，并愿意自行承担来源风险" /><Group justify="flex-end"><Button variant="default" onClick={developerModeModalHandlers.close}>取消</Button><Button color="orange" loading={setDeveloperModeMutation.isPending} disabled={!developerRiskAccepted} onClick={() => setDeveloperMode(true)}>确认开启</Button></Group></Stack></Modal></PageShell>
}
