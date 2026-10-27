import { apiFetch } from './client'
import type { User } from './types'

export interface SetupPayload {
  username: string
  password: string
}

export function authStatus() {
  return apiFetch<{ setup_required: boolean; user: User | null; csrf_token?: string; registration_open?: boolean }>('/auth/status')
}

export function login(username: string, password: string) {
  return apiFetch<{ user: User; csrf_token: string }>('/auth/login', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  })
}

export function logout() {
  return apiFetch<void>('/auth/logout', { method: 'POST' })
}

export function setup(payload: SetupPayload) {
  return apiFetch<{ user: User; csrf_token: string }>('/auth/setup', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}
