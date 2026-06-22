import { get, post } from './client'
import type {
  AuthMeResponse,
  CaptchaConfigResponse,
  ChangePasswordRequest,
  Login2FARequest,
  LoginRecoverRequest,
  LoginRequest,
  LoginResponse,
  SetupAdminRequest,
  TwoFADisableRequest,
  TwoFAEnableRequest,
  TwoFAEnableResponse,
  TwoFASetupResponse,
  TwoFAStatusResponse,
} from './types'

export function setupAdmin(data: SetupAdminRequest): Promise<{ username: string; login_path: string }> {
  return post('/setup/admin', data)
}

export function login(data: LoginRequest): Promise<LoginResponse> {
  return post('/auth/login', data)
}

export function login2FA(data: Login2FARequest): Promise<LoginResponse> {
  return post('/auth/login/2fa', data)
}

export function loginRecover(data: LoginRecoverRequest): Promise<LoginResponse> {
  return post('/auth/login/recover', data)
}

export function getAuthMe(): Promise<AuthMeResponse> {
  return get('/auth/me')
}

export function getCaptchaConfig(): Promise<CaptchaConfigResponse> {
  return get('/auth/captcha-config')
}

export function logout(): Promise<{ logged_out: boolean }> {
  return post('/auth/logout')
}

export function get2FAStatus(): Promise<TwoFAStatusResponse> {
  return get('/auth/2fa/status')
}

export function setup2FA(): Promise<TwoFASetupResponse> {
  return post('/auth/2fa/setup')
}

export function enable2FA(data: TwoFAEnableRequest): Promise<TwoFAEnableResponse> {
  return post('/auth/2fa/enable', data)
}

export function disable2FA(data: TwoFADisableRequest): Promise<{ disabled: boolean }> {
  return post('/auth/2fa/disable', data)
}

export function regenerateRecoveryCodes(code: string): Promise<{ recovery_codes: string[] }> {
  return post('/auth/2fa/regenerate-codes', { code })
}

export function changePassword(data: ChangePasswordRequest): Promise<{ changed: boolean }> {
  return post('/auth/change-password', data)
}
