import { useQuery } from '@tanstack/react-query'
import { getSite } from '@/api/sites'

export const siteDetailKeys = {
  detail: (siteId: string | null | undefined) => ['site-detail', siteId] as const,
}

export function useSiteDetail(siteId: string | null, opened: boolean) {
  return useQuery({
    // 每个详情请求都把 siteId 放进 query key，避免快速切换站点时复用到上一站点缓存。
    queryKey: siteDetailKeys.detail(siteId),
    queryFn: () => getSite(siteId!),
    enabled: opened && Boolean(siteId),
  })
}
