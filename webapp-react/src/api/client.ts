import axios, { AxiosError, type AxiosRequestConfig, type AxiosResponse } from 'axios'
import { gateSecret } from './gate'

export interface ApiResponse<T> {
  request_id: string
  success: boolean
  data: T | null
  error: null | {
    code: string
    message: string
    details?: Record<string, unknown>
  }
}

export class ApiError extends Error {
  request_id?: string
  code: string
  details?: Record<string, unknown>
  status?: number

  constructor(error: { code: string; message: string; request_id?: string; details?: Record<string, unknown>; status?: number }) {
    super(error.message)
    this.name = 'ApiError'
    this.code = error.code
    this.request_id = error.request_id
    this.details = error.details
    this.status = error.status
  }
}

export const http = axios.create({
  baseURL: `/api/v1/${gateSecret}`,
  timeout: 30_000,
  withCredentials: true,
})

const CSRF_METHODS = ['post', 'put', 'patch', 'delete']

function getCSRFCookie(): string {
  if (typeof document === 'undefined') return ''

  const match = document.cookie
    .split('; ')
    .find((row) => row.startsWith('csrf-token='))
  return match ? match.split('=').slice(1).join('=') : ''
}

http.interceptors.request.use((config) => {
  const method = (config.method || '').toLowerCase()
  if (CSRF_METHODS.includes(method)) {
    const token = getCSRFCookie()
    if (token) {
      // 写请求沿用旧前端的 cookie CSRF 方案，但 token 只放入请求头，不进入日志或通知。
      config.headers['X-CSRF-Token'] = token
    }
  }
  return config
})

http.interceptors.response.use(
  (resp: AxiosResponse) => {
    const body = resp.data as ApiResponse<unknown> | undefined
    if (!body || typeof body.success !== 'boolean') {
      return resp
    }

    if (!body.success) {
      return Promise.reject(new ApiError({
        request_id: body.request_id,
        code: body.error?.code || 'UNKNOWN',
        message: body.error?.message || '请求失败',
        details: body.error?.details,
        status: resp.status,
      }))
    }

    resp.data = body.data
    return resp
  },
  (err: AxiosError<ApiResponse<unknown>>) => {
    const body = err.response?.data
    const status = err.response?.status

    // 401 只标准化错误，不在 API 层硬跳转，后续由 AuthProvider/ProtectedRoute 统一清理状态。
    if (status === 401) {
      return Promise.reject(new ApiError({
        request_id: body?.request_id,
        code: body?.error?.code || 'AUTH_REQUIRED',
        message: body?.error?.message || '请先登录',
        details: body?.error?.details,
        status,
      }))
    }

    return Promise.reject(new ApiError({
      request_id: body?.request_id,
      code: body?.error?.code || 'NETWORK_ERROR',
      message: body?.error?.message || err.message || '网络错误',
      details: body?.error?.details,
      status,
    }))
  }
)

export function get<T>(url: string, config?: AxiosRequestConfig): Promise<T> {
  return http.get<T>(url, config).then((r) => r.data)
}

export function post<T>(url: string, data?: unknown, config?: AxiosRequestConfig): Promise<T> {
  return http.post<T>(url, data, config).then((r) => r.data)
}

export function put<T>(url: string, data?: unknown, config?: AxiosRequestConfig): Promise<T> {
  return http.put<T>(url, data, config).then((r) => r.data)
}

export function del<T>(url: string, config?: AxiosRequestConfig): Promise<T> {
  return http.delete<T>(url, config).then((r) => r.data)
}
