export type CaptchaProvider = 'none' | 'turnstile' | 'hcaptcha'

export interface SecuritySettingsFormValues {
  login_path: string
  public_health: boolean
  rate_limit_max_failures: number
  rate_limit_window: string
  max_sessions: number
  bind_session_ip: boolean
  bind_session_ua: boolean
  trusted_proxies: string[]
  captcha_provider: CaptchaProvider
  captcha_site_key: string
  captcha_secret_key: string
  captcha_trigger_after_failures: number
  tls_enabled: boolean
  tls_cert: string
  tls_key: string
  tls_cert_validity: string
}
