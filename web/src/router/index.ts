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
      { path: 'monitors/new', name: 'monitor-new', component: () => import('@/views/MonitorEditView.vue') },
      { path: 'monitors/:id', name: 'monitor-detail', component: () => import('@/views/MonitorDetailView.vue') },
      { path: 'monitors/:id/edit', name: 'monitor-edit', component: () => import('@/views/MonitorEditView.vue') },
      { path: 'channels', name: 'channels', component: () => import('@/views/ChannelListView.vue') },
      { path: 'notifications', name: 'notifications', component: () => import('@/views/NotificationListView.vue') },
      { path: 'import', name: 'import', component: () => import('@/views/ImportView.vue') },
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
    // 已登录用户不回登录页；但 setup 页面始终可达（后端会再次校验）
    if ((to.name === 'login' || to.name === 'setup') && auth.user) return { path: '/' }
    return true
  }
  if (!auth.user) return { path: '/login', query: { redirect: to.fullPath } }
  if (to.meta.admin && auth.user.role !== 'admin') return { path: '/' }
  return true
})
