import { apiFetch } from './client'

export interface MailConfig {
  host: string
  port: string
  username: string
  password: string // 写入用；读取始终为空
  from: string
  encryption: 'starttls' | 'ssl' | 'none'
}

export function getMailConfig() {
  return apiFetch<{ config: Omit<MailConfig, 'password'>; password_set: boolean }>('/mail-config')
}

export function putMailConfig(p: Partial<MailConfig>) {
  return apiFetch<{ ok: boolean }>('/mail-config', { method: 'PUT', body: JSON.stringify(p) })
}

export function sendTestMail() {
  return apiFetch<{ ok: boolean; error?: string }>('/mail-config/test', { method: 'POST' })
}
