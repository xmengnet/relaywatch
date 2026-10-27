import { apiFetch } from './client'

export function authCsrf() {
  return apiFetch<{ csrf_token: string }>('/auth/csrf')
}
