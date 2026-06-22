import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getAuthMe } from './auth'
import { getLoginAudit, type LoginAuditQueryParams } from './logs'
import { getNginxConf, getNginxParameters } from './nginx'
import { getOperations, type OperationsQueryParams } from './operations'
import { getSecuritySettings } from './settings'
import { getSites, type SitesQueryParams } from './sites'
import { getSystemOverview, getUpgradeStatus, triggerUpgradeCheck } from './system'

export const queryKeys = {
  authMe: ['auth', 'me'] as const,
  systemOverview: ['system', 'overview'] as const,
  upgradeStatus: ['system', 'upgrade'] as const,
  sites: (params?: SitesQueryParams) => ['sites', params ?? {}] as const,
  operationLogs: (params?: OperationsQueryParams) => ['operations', params ?? {}] as const,
  loginAudit: (params?: LoginAuditQueryParams) => ['login-audit', params ?? {}] as const,
  nginxParameters: ['nginx', 'parameters'] as const,
  nginxConf: ['nginx', 'conf'] as const,
  securitySettings: ['settings', 'security'] as const,
}

export function useAuthMe() {
  return useQuery({
    queryKey: queryKeys.authMe,
    queryFn: getAuthMe,
  })
}

export function useSystemOverview() {
  return useQuery({
    queryKey: queryKeys.systemOverview,
    queryFn: getSystemOverview,
  })
}

export function useUpgradeStatus(refetchInterval?: number) {
  return useQuery({
    queryKey: queryKeys.upgradeStatus,
    queryFn: getUpgradeStatus,
    refetchInterval,
  })
}

// useUpgradeCheckMutation 手动触发升级检查。成功后刷新 upgradeStatus 查询缓存。
export function useUpgradeCheckMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: triggerUpgradeCheck,
    onSuccess: (data) => {
      queryClient.setQueryData(queryKeys.upgradeStatus, data)
    },
  })
}

export function useSites(params?: SitesQueryParams) {
  return useQuery({
    queryKey: queryKeys.sites(params),
    queryFn: () => getSites(params),
  })
}

export function useOperationLogs(params?: OperationsQueryParams) {
  return useQuery({
    queryKey: queryKeys.operationLogs(params),
    queryFn: () => getOperations(params),
  })
}

export function useLoginAudit(params?: LoginAuditQueryParams) {
  return useQuery({
    queryKey: queryKeys.loginAudit(params),
    queryFn: () => getLoginAudit(params),
  })
}

export function useNginxParameters(enabled = true) {
  return useQuery({
    queryKey: queryKeys.nginxParameters,
    queryFn: getNginxParameters,
    enabled,
  })
}

export function useNginxConf(enabled = true) {
  return useQuery({
    queryKey: queryKeys.nginxConf,
    queryFn: getNginxConf,
    enabled,
  })
}

export function useSecuritySettings() {
  return useQuery({
    queryKey: queryKeys.securitySettings,
    queryFn: getSecuritySettings,
  })
}
