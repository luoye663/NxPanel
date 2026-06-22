import { MantineProvider } from '@mantine/core'
import { DatesProvider } from '@mantine/dates'
import { ModalsProvider } from '@mantine/modals'
import { Notifications } from '@mantine/notifications'
import { QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from 'react-router-dom'
import 'dayjs/locale/zh-cn'
import { queryClient } from '@/api/queryClient'
import { AuthProvider } from '@/auth/AuthProvider'
import { router } from '@/router'
import { theme } from '@/theme'

export default function App() {
  return (
    <MantineProvider theme={theme} defaultColorScheme="light">
      <DatesProvider settings={{ locale: 'zh-cn' }}>
        <ModalsProvider>
          <QueryClientProvider client={queryClient}>
            {/* Provider 顺序固定：UI 外壳先挂载，路由页面才能安全使用通知、确认和 Query。 */}
            <AuthProvider>
              <RouterProvider router={router} />
            </AuthProvider>
            <Notifications position="top-right" />
          </QueryClientProvider>
        </ModalsProvider>
      </DatesProvider>
    </MantineProvider>
  )
}
