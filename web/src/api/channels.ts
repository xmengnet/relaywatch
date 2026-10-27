import { apiFetch } from './client'

export interface Channel {
  id: number
  name: string
  type: string
  config: string
  template?: string
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface NotifierMeta {
  type: string
  label: string
  icon?: string
  fields: Array<{
    key: string
    label: string
    type: string
    required: boolean
    options?: string[]
    placeholder?: string
    help?: string
  }>
}

export interface Delivery {
  id: number
  monitor_id: number
  channel_id: number
  change_id?: number
  payload?: string
  status: 'pending' | 'sent' | 'failed' | 'deferred' | 'canceled'
  attempts: number
  next_attempt_at?: string
  last_error?: string
  created_at: string
  sent_at?: string
}

export function listChannels() {
  return apiFetch<{ items: Channel[] }>('/channels')
}

export function getNotifierTypes() {
  return apiFetch<{ items: NotifierMeta[] }>('/notifiers/types')
}

export function createChannel(p: { name: string; type: string; config: Record<string, unknown>; template?: string }) {
  return apiFetch<Channel>('/channels', { method: 'POST', body: JSON.stringify(p) })
}

export function updateChannel(id: number, p: { name: string; type: string; config: Record<string, unknown>; template?: string; enabled?: boolean }) {
  return apiFetch<Channel>(`/channels/${id}`, { method: 'PATCH', body: JSON.stringify(p) })
}

export function deleteChannel(id: number) {
  return apiFetch<void>(`/channels/${id}`, { method: 'DELETE' })
}

export function testChannel(id: number) {
  return apiFetch<{ ok: boolean; error?: string }>(`/channels/${id}/test`, { method: 'POST' })
}

export function listDeliveries(params: Record<string, string | number | undefined> = {}) {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== '') q.set(k, String(v))
  }
  return apiFetch<{ items: Delivery[]; total: number }>(`/deliveries?${q}`)
}

export function resendDelivery(id: number) {
  return apiFetch<Delivery>(`/deliveries/${id}/resend`, { method: 'POST' })
}

export function getSettings() {
  return apiFetch<{ alert_channel_ids: number[] }>('/settings')
}

export function putSettings(p: { alert_channel_ids: number[] }) {
  return apiFetch<{ alert_channel_ids: number[] }>('/settings', { method: 'PUT', body: JSON.stringify(p) })
}
