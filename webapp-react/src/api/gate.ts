declare global {
  interface Window {
    __NX_GATE_PATH__?: string
  }
}

function normalizeGatePath(path?: string): string {
  if (!path || !path.startsWith('/')) return ''
  return path
}

export const gatePath = normalizeGatePath(
  typeof window !== 'undefined' ? window.__NX_GATE_PATH__ : undefined,
) || normalizeGatePath(import.meta.env.VITE_NX_GATE_PATH)
export const gateSecret = gatePath.replace(/^\//, '')
export const hasGatePath = gatePath !== ''

export function withGatePrefix(path: string): string {
  if (!hasGatePath) {
    throw new Error('缺少隐藏登录路径：生产环境应由后端注入 window.__NX_GATE_PATH__，开发环境请设置 VITE_NX_GATE_PATH。')
  }
  const normalizedPath = path.startsWith('/') ? path : `/${path}`
  return `/api/v1/${gateSecret}${normalizedPath}`
}

export function generateLoginPathSuggestion(): string {
  const alphabet = '0123456789abcdefghjkmnpqrstvwxyz'
  const bytes = new Uint8Array(13)
  crypto.getRandomValues(bytes)
  return `/nx-${Array.from(bytes, (byte) => alphabet[byte % alphabet.length]).join('')}`
}

export function reloadToLogin(path = gatePath) {
  if (!path) return
  window.location.replace(path)
}

export {}
