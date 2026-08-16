import { createContext, useContext, useEffect, useMemo, type PropsWithChildren } from 'react'
import { useBrandingQuery } from '@/api/hooks'
import type { BrandingSettings } from '@/api/types'

export const defaultBranding: BrandingSettings = {
  site_name: 'NxPanel',
  subtitle: '开源 Nginx 网站管理面板',
}

interface BrandingContextValue {
  branding: BrandingSettings
  isLoading: boolean
  isError: boolean
  error: Error | null
}

const BrandingContext = createContext<BrandingContextValue | null>(null)

export function BrandingProvider({ children }: PropsWithChildren) {
  const query = useBrandingQuery()
  const branding = query.data ?? defaultBranding

  useEffect(() => {
    document.title = branding.site_name
  }, [branding.site_name])

  const value = useMemo<BrandingContextValue>(() => ({
    branding,
    isLoading: query.isLoading,
    isError: query.isError,
    error: query.error,
  }), [branding, query.error, query.isError, query.isLoading])

  return <BrandingContext.Provider value={value}>{children}</BrandingContext.Provider>
}

export function useBranding() {
  const context = useContext(BrandingContext)
  if (!context) throw new Error('useBranding must be used inside BrandingProvider')
  return context
}
