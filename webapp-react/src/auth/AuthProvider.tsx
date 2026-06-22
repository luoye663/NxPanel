import { Alert, Center, Loader } from '@mantine/core'
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type PropsWithChildren } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { queryClient } from '@/api/queryClient'
import { ApiError, http } from '@/api/client'
import { gatePath, hasGatePath } from '@/api/gate'
import * as authApi from '@/api/auth'
import type { LoginResponse, SetupAdminRequest } from '@/api/types'

interface AuthState {
  initialized: boolean
  authenticated: boolean
  needsSetup: boolean
  username: string
  totpEnabled: boolean
}

interface AuthContextValue extends AuthState {
  refreshAuth: () => Promise<void>
  setupAdmin: (data: SetupAdminRequest) => Promise<{ username: string; login_path: string }>
  login: (username: string, password: string, captchaToken?: string) => Promise<LoginResponse>
  login2FA: (tempToken: string, code: string) => Promise<void>
  loginRecover: (tempToken: string, recoveryCode: string) => Promise<void>
  logout: () => Promise<void>
  clearAuth: () => void
}

const defaultState: AuthState = {
  initialized: false,
  authenticated: false,
  needsSetup: false,
  username: '',
  totpEnabled: false,
}

// 认证状态放在 Context 中，避免再引入类似 Pinia/Zustand 的第二套全局 store。
const InternalAuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: PropsWithChildren) {
  const [state, setState] = useState<AuthState>(defaultState)

  const clearAuth = useCallback(() => {
    setState((current) => ({
      ...current,
      initialized: true,
      authenticated: false,
      username: '',
      totpEnabled: false,
    }))
  }, [])

  const refreshAuth = useCallback(async () => {
    try {
      const me = await authApi.getAuthMe()
      setState({
        initialized: true,
        authenticated: me.authenticated,
        needsSetup: !!me.needs_setup,
        username: me.username || '',
        totpEnabled: !!me.totp_enabled,
      })
    } catch {
      clearAuth()
    }
  }, [clearAuth])

  useEffect(() => {
    void refreshAuth()
  }, [refreshAuth])

  useEffect(() => {
    const interceptorId = http.interceptors.response.use(undefined, (error) => {
      if (error instanceof ApiError && (error.code === 'AUTH_REQUIRED' || error.status === 401)) {
        // 任何页面收到 401 都清理本地认证态，但不在拦截器里跳转，避免绕过路由守卫。
        clearAuth()
        void queryClient.clear()
      }
      return Promise.reject(error)
    })

    return () => http.interceptors.response.eject(interceptorId)
  }, [clearAuth])

  const setupAdmin = useCallback(async (data: SetupAdminRequest) => {
    const resp = await authApi.setupAdmin(data)
    setState((current) => ({ ...current, initialized: true, needsSetup: false }))
    return resp
  }, [])

  const login = useCallback(async (username: string, password: string, captchaToken?: string) => {
    const resp = await authApi.login({ username, password, captcha_token: captchaToken })
    if (!resp.requires_2fa) {
      setState({ initialized: true, authenticated: true, needsSetup: false, username: resp.username, totpEnabled: false })
    }
    return resp
  }, [])

  const login2FA = useCallback(async (tempToken: string, code: string) => {
    const resp = await authApi.login2FA({ temp_token: tempToken, code })
    setState({ initialized: true, authenticated: true, needsSetup: false, username: resp.username, totpEnabled: true })
  }, [])

  const loginRecover = useCallback(async (tempToken: string, recoveryCode: string) => {
    const resp = await authApi.loginRecover({ temp_token: tempToken, recovery_code: recoveryCode })
    setState({ initialized: true, authenticated: true, needsSetup: false, username: resp.username, totpEnabled: true })
  }, [])

  const logout = useCallback(async () => {
    try {
      await authApi.logout()
    } finally {
      clearAuth()
      queryClient.clear()
    }
  }, [clearAuth])

  const value = useMemo<AuthContextValue>(() => ({
    ...state,
    refreshAuth,
    setupAdmin,
    login,
    login2FA,
    loginRecover,
    logout,
    clearAuth,
  }), [state, refreshAuth, setupAdmin, login, login2FA, loginRecover, logout, clearAuth])

  return <InternalAuthContext.Provider value={value}>{children}</InternalAuthContext.Provider>
}

export function useAuth() {
  const context = useContext(InternalAuthContext)
  if (!context) {
    throw new Error('useAuth must be used inside AuthProvider')
  }
  return context
}

function AuthLoading({ compact = false }: { compact?: boolean }) {
  return (
    <Center h={compact ? 120 : '100vh'}>
      <Loader aria-label="正在加载认证状态" />
    </Center>
  )
}

export function ProtectedRoute({ children }: PropsWithChildren) {
	const auth = useAuth()
	const location = useLocation()

	if (!auth.initialized) return <AuthLoading compact />
	if (!hasGatePath) return <GatePathMissing />
	if (auth.needsSetup && !auth.authenticated) return <Navigate to={gatePath} replace />
  if (!auth.authenticated) {
    const redirect = location.pathname + location.search
    return <Navigate to={`${gatePath}?redirect=${encodeURIComponent(redirect)}`} replace />
  }

  return children
}

export function PublicOnlyRoute({ children }: PropsWithChildren) {
	const auth = useAuth()

	if (!auth.initialized) return <AuthLoading compact />
	if (!hasGatePath) return <GatePathMissing />
	if (auth.authenticated) return <Navigate to="/" replace />
  if (auth.needsSetup) return <Navigate to={gatePath} replace />

  return children
}

export function SetupRoute({ children }: PropsWithChildren) {
	const auth = useAuth()

	if (!auth.initialized) return <AuthLoading />
	if (!hasGatePath) return <GatePathMissing />
	if (auth.authenticated) return <Navigate to="/" replace />
  if (!auth.needsSetup) return <Navigate to={gatePath} replace />

	return children
}

function GatePathMissing() {
	return (
		<Center h="100vh" p="md">
			<Alert color="red" title="缺少隐藏登录路径">
				生产环境请从后端隐藏入口访问页面；Vite 开发环境请通过 make dev-frontend 自动注入，或手动设置 VITE_NX_GATE_PATH。
			</Alert>
		</Center>
	)
}
