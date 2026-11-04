import { apiFetch } from './client'

export interface APIToken {
  id: number
  name: string
  created_at: string
  expires_at?: string
  last_used_at?: string
}

export function listAPITokens() {
  return apiFetch<{ items: APIToken[] }>('/tokens')
}

export function createAPIToken(payload: { name: string; expires_at?: string }) {
  return apiFetch<{ token: string }>('/tokens', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function revokeAPIToken(id: number | string) {
  return apiFetch<void>(`/tokens/${id}`, { method: 'DELETE' })
}
