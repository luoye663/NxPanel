import { QueryClient } from '@tanstack/react-query'

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 15_000,
      retry: 1,
    },
    mutations: {
      // 写操作不能自动重试，避免重复提交危险配置变更。
      retry: false,
    },
  },
})
