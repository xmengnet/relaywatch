import { apiFetch } from './client'
import type { AdminUser, Role, User } from './types'

// ---------- 用户管理（admin） ----------

export function listUsers(params: { q?: string; role?: string; page?: number; per_page?: number } = {}) {
  const qs = new URLSearchParams()
  if (params.q) qs.set('q', params.q)
  if (params.role) qs.set('role', params.role)
  if (params.page) qs.set('page', String(params.page))
  if (params.per_page) qs.set('per_page', String(params.per_page))
  const query = qs.toString()
  return apiFetch<{ items: AdminUser[]; total: number }>(`/users${query ? `?${query}` : ''}`)
}

export function createUser(p: { username: string; password: string; email?: string; display_name?: string; role?: Role; permissions?: string[] }) {
  return apiFetch<AdminUser>('/users', { method: 'POST', body: JSON.stringify(p) })
}

export function updateUser(id: number, p: { email?: string; display_name?: string; role?: Role; password?: string; permissions?: string[] }) {
  return apiFetch<AdminUser>(`/users/${id}`, { method: 'PATCH', body: JSON.stringify(p) })
}

export function setUserDisabled(id: number, disabled: boolean) {
  return apiFetch<AdminUser>(`/users/${id}/disable`, { method: 'POST', body: JSON.stringify({ disabled }) })
}

export function deleteUser(id: number) {
  return apiFetch<{ ok: boolean; transferred_to: string }>(`/users/${id}`, { method: 'DELETE' })
}

// ---------- 个人资料（所有角色） ----------

export function getProfile() {
  return apiFetch<{ user: User; oidc: boolean; oidc_provider?: string; has_password?: boolean }>('/profile')
}

// 解绑当前用户的 OIDC 身份（未设本地密码的纯 OIDC 账户会被后端拒绝）
export function unbindOIDC() {
  return apiFetch<{ ok: boolean }>('/profile/oidc', { method: 'DELETE' })
}

export function updateProfile(p: { email?: string; display_name?: string }) {
  return apiFetch<{ user: User }>('/profile', { method: 'PATCH', body: JSON.stringify(p) })
}

export function changePassword(p: { old_password: string; new_password: string }) {
  return apiFetch<{ ok: boolean }>('/profile/password', { method: 'POST', body: JSON.stringify(p) })
}

export interface SessionInfo {
  id: string
  user_agent?: string
  ip?: string
  created_at: string
  expires_at: string
  current: boolean
}

export function listSessions() {
  return apiFetch<{ items: SessionInfo[] }>('/profile/sessions')
}

export function revokeSession(id: string) {
  return apiFetch<{ ok: boolean }>(`/profile/sessions/${id}`, { method: 'DELETE' })
}

export function sendRegisterCode(email: string) {
  return apiFetch<{ ok: boolean }>('/auth/register/send-code', { method: 'POST', body: JSON.stringify({ email }) })
}

export function register(p: { username: string; password: string; email?: string; email_code?: string }) {
  return apiFetch<{ user: User; csrf_token: string }>('/auth/register', {
    method: 'POST',
    body: JSON.stringify(p),
  })
}
