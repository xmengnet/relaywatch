import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import DefaultLayout from '@/layouts/DefaultLayout.vue'

const routes = [
  {
    path: '/',
    component: DefaultLayout,
    children: [
      { path: '', name: 'dashboard', component: () => import('@/views/DashboardView.vue') },
      { path: 'monitors', name: 'monitors', component: () => import('@/views/MonitorListView.vue') },
      // perm/write 见路由守卫：前者按细粒度权限（可被 admin 授予），
      // 后者按写权限（viewer 只读，编辑既有监控一律拒绝）
      { path: 'monitors/new', name: 'monitor-new', component: () => import('@/views/MonitorEditView.vue'), meta: { perm: 'monitor.create' } },
      { path: 'monitors/:id', name: 'monitor-detail', component: () => import('@/views/MonitorDetailView.vue') },
      { path: 'monitors/:id/edit', name: 'monitor-edit', component: () => import('@/views/MonitorEditView.vue'), meta: { write: true } },
      { path: 'channels', name: 'channels', component: () => import('@/views/ChannelListView.vue') },
      { path: 'notifications', name: 'notifications', component: () => import('@/views/NotificationListView.vue') },
      { path: 'import', name: 'import', component: () => import('@/views/ImportView.vue') },
      { path: 'help', name: 'help', component: () => import('@/views/HelpView.vue') },
      { path: 'profile', name: 'profile', component: () => import('@/views/ProfileView.vue') },
      { path: 'site', name: 'site', component: () => import('@/views/SiteConfigView.vue'), meta: { admin: true } },
      { path: 'users', name: 'users', component: () => import('@/views/UserListView.vue'), meta: { admin: true } },
      { path: 'settings', name: 'settings', component: () => import('@/views/SettingsView.vue'), meta: { admin: true } },
    ],
  },
  {
    path: '/',
    component: () => import('@/layouts/BlankLayout.vue'),
    children: [
      { path: 'login', name: 'login', component: () => import('@/views/LoginView.vue'), meta: { public: true } },
      { path: 'setup', name: 'setup', component: () => import('@/views/SetupView.vue'), meta: { public: true } },
    ],
  },
  { path: '/:pathMatch(.*)*', name: 'not-found', component: () => import('@/views/NotFoundView.vue') },
]

export const router = createRouter({
  history: createWebHistory(),
  routes,
})

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (!auth.loaded) await auth.load()
  if (to.meta.public) {
    // 已登录用户不回登录/引导页
    if ((to.name === 'login' || to.name === 'setup') && auth.user) return { path: '/' }
    // 首次启动必须先创建管理员账户
    if (to.name !== 'setup' && auth.setupRequired) return { path: '/setup' }
    // 系统已初始化后 setup 关闭
    if (to.name === 'setup' && !auth.setupRequired) return { path: '/login' }
    return true
  }
  if (!auth.user) {
    // 首次启动时任何页面都引导去 /setup
    if (auth.setupRequired) return { path: '/setup' }
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  if (to.meta.admin && auth.user.role !== 'admin') return { path: '/' }
  if (to.meta.perm && !auth.can(to.meta.perm as string)) return { path: '/' }
  if (to.meta.write && !auth.canWrite()) return { path: '/' }
  return true
})
