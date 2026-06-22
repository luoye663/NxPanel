import { Alert, Center, Loader } from '@mantine/core'
import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react'

type CaptchaProvider = 'turnstile' | 'hcaptcha' | ''

interface CaptchaWidgetProps {
  provider: CaptchaProvider | string
  siteKey: string
  onVerified: (token: string) => void
  onExpired?: () => void
  onError?: () => void
}

export interface CaptchaWidgetRef {
  reset: () => void
  loading: boolean
}

type CaptchaApi = {
  render: (container: HTMLElement, options: Record<string, unknown>) => string
  reset: (widgetId: string) => void
  remove: (widgetId: string) => void
}

declare global {
  interface Window {
    turnstile?: CaptchaApi
    hcaptcha?: CaptchaApi
    onNxPanelTurnstileLoad?: () => void
  }
}

let turnstileScriptPromise: Promise<void> | null = null
let hcaptchaScriptPromise: Promise<void> | null = null

function loadCaptchaScript(provider: CaptchaProvider | string): Promise<void> {
  if (provider === 'turnstile') {
    if (window.turnstile) return Promise.resolve()
    if (turnstileScriptPromise) return turnstileScriptPromise

    turnstileScriptPromise = new Promise((resolve, reject) => {
      const script = document.createElement('script')
      script.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit&onload=onNxPanelTurnstileLoad'
      script.async = true
      script.defer = true
      window.onNxPanelTurnstileLoad = () => resolve()
      script.onerror = () => {
        turnstileScriptPromise = null
        reject(new Error('Turnstile 加载失败'))
      }
      document.head.appendChild(script)
    })
    return turnstileScriptPromise
  }

  if (provider === 'hcaptcha') {
    if (window.hcaptcha) return Promise.resolve()
    if (hcaptchaScriptPromise) return hcaptchaScriptPromise

    hcaptchaScriptPromise = new Promise((resolve, reject) => {
      const script = document.createElement('script')
      script.src = 'https://js.hcaptcha.com/1/api.js'
      script.async = true
      script.defer = true
      script.onload = () => resolve()
      script.onerror = () => {
        hcaptchaScriptPromise = null
        reject(new Error('hCaptcha 加载失败'))
      }
      document.head.appendChild(script)
    })
    return hcaptchaScriptPromise
  }

  return Promise.resolve()
}

export const CaptchaWidget = forwardRef<CaptchaWidgetRef, CaptchaWidgetProps>(function CaptchaWidget(
  { provider, siteKey, onVerified, onExpired, onError },
  ref
) {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const widgetIdRef = useRef<string | null>(null)
  const onVerifiedRef = useRef(onVerified)
  const onExpiredRef = useRef(onExpired)
  const onErrorRef = useRef(onError)
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState(false)

  useEffect(() => {
    onVerifiedRef.current = onVerified
    onExpiredRef.current = onExpired
    onErrorRef.current = onError
  }, [onVerified, onExpired, onError])

  useImperativeHandle(ref, () => ({
    loading,
    reset: () => {
      if (provider === 'turnstile' && window.turnstile && widgetIdRef.current) {
        window.turnstile.reset(widgetIdRef.current)
      }
      if (provider === 'hcaptcha' && window.hcaptcha && widgetIdRef.current) {
        window.hcaptcha.reset(widgetIdRef.current)
      }
    },
  }), [loading, provider])

  useEffect(() => {
    if (!provider || !siteKey || !containerRef.current) return undefined

    let cancelled = false
    setLoading(true)
    setLoadError(false)

    loadCaptchaScript(provider)
      .then(() => {
        if (cancelled || !containerRef.current) return
        const options = {
          sitekey: siteKey,
          callback: (token: string) => onVerifiedRef.current(token),
          'expired-callback': () => onExpiredRef.current?.(),
          'error-callback': () => onErrorRef.current?.(),
        }
        try {
          if (provider === 'turnstile' && window.turnstile) {
            widgetIdRef.current = window.turnstile.render(containerRef.current, options)
          }
          if (provider === 'hcaptcha' && window.hcaptcha) {
            widgetIdRef.current = window.hcaptcha.render(containerRef.current, options)
          }
        } catch {
          setLoadError(true)
        }
      })
      .catch(() => {
        if (!cancelled) setLoadError(true)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => {
      cancelled = true
      if (provider === 'turnstile' && window.turnstile && widgetIdRef.current) {
        window.turnstile.remove(widgetIdRef.current)
      }
      if (provider === 'hcaptcha' && window.hcaptcha && widgetIdRef.current) {
        window.hcaptcha.remove(widgetIdRef.current)
      }
      widgetIdRef.current = null
      // 组件卸载时只移除当前 widget，不移除全局脚本，避免多个登录组件重复加载外部脚本。
    }
  }, [provider, siteKey])

  if (!provider || !siteKey) return null

  return (
    <div className="captchaWidget">
      {loading ? (
        <Center py="sm" c="dimmed" fz="sm"><Loader size="xs" mr="xs" />正在加载人机验证...</Center>
      ) : null}
      {loadError ? <Alert color="red">人机验证加载失败，请检查网络后刷新页面</Alert> : null}
      <div ref={containerRef} className="captchaContainer" />
    </div>
  )
})
