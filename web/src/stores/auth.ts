import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as authApi from '@/api/auth'
import { setCsrf } from '@/api/client'
import type { User } from '@/api/types'

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(null)
  const loaded = ref(false)
  const setupRequired = ref(false)
  const registrationOpen = ref(false)

  async function load() {
    const status = await authApi.authStatus()
    user.value = status.user
    setupRequired.value = status.setup_required
    registrationOpen.value = status.registration_open ?? false
    loaded.value = true
    if (status.user && status.csrf_token) {
      // 已登录（含页面刷新后）：直接取回该会话的 CSRF token
      setCsrf(status.csrf_token)
    } else if (!status.user) {
      // 页面刷新后会话可能还在，尝试拿一次 CSRF
      try {
        const { authCsrf } = await import('@/api/csrf')
        const r = await authCsrf()
        setCsrf(r.csrf_token)
      } catch {
        /* 未登录时忽略 */
      }
    }
  }

  async function login(username: string, password: string) {
    const r = await authApi.login(username, password)
    user.value = r.user
    setCsrf(r.csrf_token)
  }

  async function setup(username: string, password: string) {
    const r = await authApi.setup({ username, password })
    user.value = r.user
    setupRequired.value = false
    setCsrf(r.csrf_token)
  }

  async function logout() {
    await authApi.logout()
    user.value = null
    setCsrf('')
  }

  async function register(username: string, password: string, email?: string) {
    const { register: registerApi } = await import('@/api/users')
    const r = await registerApi({ username, password, email })
    user.value = r.user
    setCsrf(r.csrf_token)
  }

  return { user, loaded, setupRequired, registrationOpen, load, login, setup, register, logout }
})
