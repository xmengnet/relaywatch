import { apiFetch } from './client'

export function authCsrf() {
  // 未登录时该端点返回 401 属预期，不触发全局跳转
  return apiFetch<{ csrf_token: string }>('/auth/csrf', {}, { skipAuthRedirect: true })
}
