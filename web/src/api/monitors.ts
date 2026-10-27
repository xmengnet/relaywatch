import { apiFetch } from './client'
import type { Monitor, SourceMeta } from './types'

export function listMonitors(params: Record<string, string | number | undefined> = {}) {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== '') q.set(k, String(v))
  }
  return apiFetch<{ items: Monitor[]; total: number }>(`/monitors?${q}`)
}

export function getMonitor(id: string | number) {
  return apiFetch<Monitor>(`/monitors/${id}`)
}

export interface MonitorPayload {
  name: string
  source_type: string
  source_config: Record<string, unknown>
  version_prefix?: string
  version_suffix?: string
  version_include_regex?: string
  version_ignore_regex?: string
  from_pattern?: string
  to_pattern?: string
  interval_sec: number
  enabled?: boolean
  notify_cooldown_sec?: number
  tags?: string[]
  note?: string
  channel_ids?: number[]
  reference_monitor_id?: number | null
}

export function createMonitor(p: MonitorPayload) {
  return apiFetch<Monitor>('/monitors', { method: 'POST', body: JSON.stringify(p) })
}

export function updateMonitor(id: string | number, p: MonitorPayload) {
  return apiFetch<Monitor>(`/monitors/${id}`, { method: 'PATCH', body: JSON.stringify(p) })
}

export function deleteMonitor(id: string | number) {
  return apiFetch<void>(`/monitors/${id}`, { method: 'DELETE' })
}

export function checkNow(id: string | number) {
  return apiFetch<{ ok: boolean; version?: string; state: string; error?: string; duration_ms: number; skipped?: boolean }>(
    `/monitors/${id}/check`, { method: 'POST' })
}

export function ackMonitor(id: string | number) {
  return apiFetch<Monitor>(`/monitors/${id}/ack`, { method: 'POST' })
}

export function batchMonitors(action: string, ids: number[]) {
  return apiFetch<{ affected: number }>('/monitors/batch', {
    method: 'POST',
    body: JSON.stringify({ action, ids }),
  })
}

export interface CheckHistoryItem {
  id: number
  monitor_id: number
  checked_at: string
  duration_ms: number
  ok: boolean
  version?: string
  error?: string
}

export interface VersionChangeItem {
  id: number
  monitor_id: number
  old_version?: string
  new_version: string
  detected_at: string
  acknowledged_at?: string
}

export function listHistory(id: string | number, page = 1, perPage = 20) {
  return apiFetch<{ items: CheckHistoryItem[]; total: number }>(
    `/monitors/${id}/history?page=${page}&per_page=${perPage}`)
}

export function listChanges(id: string | number, page = 1, perPage = 20) {
  return apiFetch<{ items: VersionChangeItem[]; total: number }>(
    `/monitors/${id}/changes?page=${page}&per_page=${perPage}`)
}

export function getSourceTypes() {
  return apiFetch<{ items: SourceMeta[] }>('/sources/types')
}

export function validateSource(sourceType: string, sourceConfig: Record<string, unknown>) {
  return apiFetch<{ valid: boolean; error?: string }>('/monitors/validate', {
    method: 'POST',
    body: JSON.stringify({ source_type: sourceType, source_config: sourceConfig }),
  })
}

export function getStats() {
  return apiFetch<{ total: number; has_update: number; error: number; ok: number; missing: number; unknown: number }>('/stats')
}
