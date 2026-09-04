import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  callPluginRPC,
  bindPluginAuthorization,
  createPluginAuthorizationDevice,
  deletePluginAuthorization,
  getPluginDeveloperMode,
  getPluginAuthorizations,
  getPluginRepositoryStatus,
  getPluginCatalog,
  getPluginContributions,
  getPlugins,
  installPlugin,
  inspectDeveloperPackage,
  installDeveloperPackage,
  pollPluginAuthorizationDevice,
  refreshPluginCatalog,
  setPluginEnabled,
  setPluginDeveloperMode,
  uninstallPlugin,
  updatePlugin,
  type PluginCatalogItem,
} from './plugins'

export const pluginQueryKeys = {
  all: ['plugins'] as const,
  catalog: ['plugins', 'catalog'] as const,
  installed: ['plugins', 'installed'] as const,
  contributions: ['plugins', 'contributions'] as const,
  repositoryStatus: ['plugins', 'repository-status'] as const,
  developerMode: ['plugins', 'developer-mode'] as const,
  authorizations: ['plugins', 'authorizations'] as const,
  authorizationPoll: (attemptId: string) => ['plugins', 'authorization-poll', attemptId] as const,
  rpc: (pluginId: string, method: string, context?: unknown) => ['plugins', pluginId, 'rpc', method, context ?? null] as const,
}

export function usePluginRepositoryStatus() {
  return useQuery({ queryKey: pluginQueryKeys.repositoryStatus, queryFn: getPluginRepositoryStatus })
}

export function usePluginDeveloperMode() {
  return useQuery({ queryKey: pluginQueryKeys.developerMode, queryFn: getPluginDeveloperMode })
}

export function usePluginAuthorizations(enabled = true) {
  return useQuery({ queryKey: pluginQueryKeys.authorizations, queryFn: getPluginAuthorizations, enabled })
}

export function usePluginCatalog() {
  return useQuery({ queryKey: pluginQueryKeys.catalog, queryFn: getPluginCatalog })
}

export function useInstalledPlugins() {
  return useQuery({ queryKey: pluginQueryKeys.installed, queryFn: getPlugins })
}

export function usePluginContributions() {
  return useQuery({
    queryKey: pluginQueryKeys.contributions,
    queryFn: getPluginContributions,
    staleTime: 30_000,
  })
}

export function usePluginRPC<T>(pluginId: string, method: string, context?: Record<string, unknown>, enabled = true) {
  return useQuery({
    queryKey: pluginQueryKeys.rpc(pluginId, method, context),
    queryFn: () => callPluginRPC<T>(pluginId, { method, context }),
    enabled: enabled && Boolean(pluginId),
  })
}

function useInvalidatePlugins() {
  const queryClient = useQueryClient()
  return () => queryClient.invalidateQueries({ queryKey: pluginQueryKeys.all })
}

export function useRefreshPluginCatalog() {
  const invalidate = useInvalidatePlugins()
  return useMutation({ mutationFn: refreshPluginCatalog, onSuccess: invalidate })
}

export function useInstallPlugin() {
  const invalidate = useInvalidatePlugins()
  return useMutation({
    mutationFn: ({ plugin, permissions }: { plugin: PluginCatalogItem; permissions: string[] }) => installPlugin(plugin.id, plugin.version, permissions),
    onSuccess: invalidate,
  })
}

export function useUpdatePlugin() {
  const invalidate = useInvalidatePlugins()
  return useMutation({
    mutationFn: ({ plugin, permissions }: { plugin: PluginCatalogItem; permissions: string[] }) => updatePlugin(plugin.id, plugin.version, permissions),
    onSuccess: invalidate,
  })
}

export function useTogglePlugin() {
  const invalidate = useInvalidatePlugins()
  return useMutation({
    mutationFn: ({ pluginId, enabled }: { pluginId: string; enabled: boolean }) => setPluginEnabled(pluginId, enabled),
    onSuccess: invalidate,
  })
}

export function useUninstallPlugin() {
  const invalidate = useInvalidatePlugins()
  return useMutation({
    mutationFn: ({ pluginId, purgeData }: { pluginId: string; purgeData?: boolean }) => uninstallPlugin(pluginId, purgeData),
    onSuccess: invalidate,
  })
}

export function useSetPluginDeveloperMode() {
  const invalidate = useInvalidatePlugins()
  return useMutation({
    mutationFn: ({ enabled, acknowledgeRisk }: { enabled: boolean; acknowledgeRisk: boolean }) => setPluginDeveloperMode(enabled, acknowledgeRisk),
    onSuccess: invalidate,
  })
}

export function useInspectDeveloperPackage() {
  return useMutation({ mutationFn: inspectDeveloperPackage })
}

export function useInstallDeveloperPackage() {
  const invalidate = useInvalidatePlugins()
  return useMutation({
    mutationFn: ({ uploadToken, approvedPermissions }: { uploadToken: string; approvedPermissions: string[] }) => installDeveloperPackage(uploadToken, approvedPermissions),
    onSuccess: invalidate,
  })
}

export function useCreatePluginAuthorizationDevice() {
  return useMutation({ mutationFn: ({ pluginId, version }: { pluginId: string; version: string }) => createPluginAuthorizationDevice(pluginId, version) })
}

export function usePollPluginAuthorizationDevice(attemptId: string | null, intervalSeconds = 5) {
  return useQuery({
    queryKey: pluginQueryKeys.authorizationPoll(attemptId || ''),
    queryFn: () => pollPluginAuthorizationDevice(attemptId || ''),
    enabled: Boolean(attemptId),
    refetchInterval: (query) => {
      const status = query.state.data?.status
      return status === 'authorized' || status === 'denied' || status === 'expired' ? false : Math.max(2, query.state.data?.interval || intervalSeconds) * 1000
    },
    retry: false,
  })
}

export function useBindPluginAuthorization() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ pluginId, authorizationId }: { pluginId: string; authorizationId: string }) => bindPluginAuthorization(pluginId, authorizationId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: pluginQueryKeys.authorizations })
      await queryClient.invalidateQueries({ queryKey: pluginQueryKeys.installed })
    },
  })
}

export function useDeletePluginAuthorization() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: deletePluginAuthorization,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: pluginQueryKeys.authorizations })
      await queryClient.invalidateQueries({ queryKey: pluginQueryKeys.installed })
    },
  })
}
