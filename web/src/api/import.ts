import { apiFetch } from './client'

export interface ImportPreviewItem {
  name: string
  source_type: string
  source_config: Record<string, unknown>
  version_prefix?: string
  include_regex?: string
  ignore_regex?: string
  from_pattern?: string
  to_pattern?: string
  valid: boolean
  error?: string
}

export interface ImportPreview {
  items: ImportPreviewItem[]
  warnings: string[]
  ignored: string[]
}

export function importPreview(text: string) {
  return apiFetch<ImportPreview>('/import/preview', { method: 'POST', body: JSON.stringify({ text }) })
}

export function doImport(text: string, selected: string[]) {
  return apiFetch<{ imported: number; skipped: number }>('/import', {
    method: 'POST',
    body: JSON.stringify({ text, selected }),
  })
}

export interface ExportFile {
  version: number
  exported: string
  redacted: boolean
  monitors: unknown[]
  channels: unknown[]
}

export function exportUrl(redact = true) {
  return `/api/v1/export?redact=${redact ? '1' : '0'}`
}

export interface OIDCProviderInfo {
  id?: number
  name: string
  issuer?: string
  client_id?: string
  scopes?: string
  claim_rules?: string
  icon_url?: string
  enabled?: boolean
}

export function listOIDCProviders() {
  return apiFetch<{ items: OIDCProviderInfo[] }>('/auth/providers')
}

// admin：单个 Provider 完整信息（编辑表单用，secret 不返回）
export function getOIDCProvider(id: number) {
  return apiFetch<OIDCProviderInfo>(`/auth/providers/${id}`)
}

export function createOIDCProvider(p: Record<string, unknown>) {
  return apiFetch<{ id: number }>('/auth/providers', { method: 'POST', body: JSON.stringify(p) })
}

export function updateOIDCProvider(id: number, p: Record<string, unknown>) {
  return apiFetch<void>(`/auth/providers/${id}`, { method: 'PATCH', body: JSON.stringify(p) })
}

export function deleteOIDCProvider(id: number) {
  return apiFetch<void>(`/auth/providers/${id}`, { method: 'DELETE' })
}

// admin：OIDC 回调地址的 base_url（来自 server.base_url 配置）
export function getOIDCCallbackBase() {
  return apiFetch<{ base_url: string }>('/auth/oidc/callback-url')
}
