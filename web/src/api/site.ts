import { apiFetch } from './client'

export interface MirrorRule {
  from: string
  to: string
}

export interface HostDefault {
  source_type: string
  host: string
}

export interface SiteConfig {
  mirrors: MirrorRule[]
  host_defaults: HostDefault[]
}

export function getSiteConfig() {
  return apiFetch<SiteConfig>('/site-config')
}

export function putSiteConfig(p: SiteConfig) {
  return apiFetch<SiteConfig>('/site-config', { method: 'PUT', body: JSON.stringify(p) })
}
