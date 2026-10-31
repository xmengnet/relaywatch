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
  const registrationRequireEmail = ref(false)

  async function load() {
    const status = await authApi.authStatus()
    user.value = status.user
    setupRequired.value = status.setup_required
    registrationOpen.value = status.registration_open ?? false
    registrationRequireEmail.value = status.registration_require_email ?? false
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
    try {
      await authApi.logout()
    } finally {
      // 网络失败也清本地状态：用户点登出就该离开，残留会话由服务端过期回收
      user.value = null
      setCsrf('')
    }
  }

  async function register(username: string, password: string, email?: string, emailCode?: string) {
    const { register: registerApi } = await import('@/api/users')
    const r = await registerApi({ username, password, email, email_code: emailCode })
    user.value = r.user
    setCsrf(r.csrf_token)
  }

  // 细粒度权限判定：admin 恒真；显式 permissions 优先；未配置按角色默认。
  // 与后端 store.User.Can 语义一致，前端仅用于隐藏入口（服务端仍最终校验）。
  function can(perm: string): boolean {
    const u = user.value
    if (!u) return false
    if (u.role === 'admin') return true
    const perms = u.permissions ?? (u.role === 'viewer' ? [] : ['monitor.create', 'channel.create', 'import', 'export'])
    return perms.includes(perm)
  }

  // 写权限判定：admin/user 可增删改既有资源，viewer 只读。
  // 对应后端 store.User.CanWrite；显式 permissions 只覆盖四个附加操作，不解除只读。
  function canWrite(): boolean {
    const u = user.value
    return !!u && u.role !== 'viewer'
  }

  return { user, loaded, setupRequired, registrationOpen, registrationRequireEmail, load, login, setup, register, logout, can, canWrite }
})
