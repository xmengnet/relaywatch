<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { listUsers, createUser, updateUser, setUserDisabled, deleteUser } from '@/api/users'
import type { AdminUser, Role } from '@/api/types'

const auth = useAuthStore()

const items = ref<AdminUser[]>([])
const total = ref(0)
const search = ref('')
const loading = ref(false)

const snackbar = ref(false)
const snackbarText = ref('')
const snackbarColor = ref('success')

// 新建/编辑对话框
const dialogOpen = ref(false)
const editing = ref<AdminUser | null>(null)
const saving = ref(false)
const formError = ref('')
const form = ref()
const uForm = ref({
  username: '', password: '', email: '', display_name: '', role: 'user' as Role,
})

const roleItems: Array<{ value: Role; title: string; color: string }> = [
  { value: 'admin', title: '管理员', color: 'error' },
  { value: 'user', title: '用户', color: 'primary' },
  { value: 'viewer', title: '只读', color: 'info' },
]

// 细粒度权限项（与后端 store.AllPermissions 一致）
const permissionItems = [
  { value: 'monitor.create', title: '创建监控' },
  { value: 'channel.create', title: '创建渠道' },
  { value: 'import', title: '导入（nvchecker keyfile）' },
  { value: 'export', title: '导出配置' },
]
// 编辑时：null = 按角色默认；数组 = 显式配置
const permsTouched = ref(false)
const selectedPerms = ref<string[]>([])

const permsHint = computed(() => {
  if (uForm.value.role === 'admin') return '管理员恒有全部权限'
  if (!permsTouched.value) return uForm.value.role === 'viewer' ? '未配置：只读用户默认无创建权限' : '未配置：默认允许全部'
  return '已显式配置（勾选项即全部权限）'
})

const roleColor = (role: string) => roleItems.find((r) => r.value === role)?.color ?? 'default'
const roleTitle = (role: string) => roleItems.find((r) => r.value === role)?.title ?? role

async function load() {
  loading.value = true
  try {
    const r = await listUsers({ q: search.value || undefined })
    items.value = r.items
    total.value = r.total
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), 'error')
  } finally {
    loading.value = false
  }
}

onMounted(load)

function openCreate() {
  editing.value = null
  uForm.value = { username: '', password: '', email: '', display_name: '', role: 'user' }
  permsTouched.value = false
  selectedPerms.value = []
  formError.value = ''
  dialogOpen.value = true
}

function openEdit(u: AdminUser) {
  editing.value = u
  uForm.value = { username: u.username, password: '', email: u.email ?? '', display_name: u.display_name ?? '', role: u.role }
  permsTouched.value = u.permissions != null
  selectedPerms.value = u.permissions ? [...u.permissions] : []
  formError.value = ''
  dialogOpen.value = true
}

function touchPerms() {
  permsTouched.value = true
}

async function save() {
  const { valid } = await form.value.validate()
  if (!valid) return
  saving.value = true
  formError.value = ''
  try {
    if (editing.value) {
      const p: { email: string; display_name: string; role: Role; password?: string; permissions?: string[] } = {
        email: uForm.value.email, display_name: uForm.value.display_name, role: uForm.value.role,
      }
      if (uForm.value.password) p.password = uForm.value.password
      if (permsTouched.value && uForm.value.role !== 'admin') p.permissions = selectedPerms.value
      await updateUser(editing.value.id, p)
      toast('用户已保存')
    } else {
      const p: { username: string; password: string; email: string; display_name: string; role: Role; permissions?: string[] } = { ...uForm.value }
      if (permsTouched.value && uForm.value.role !== 'admin') p.permissions = selectedPerms.value
      await createUser(p)
      toast('用户已创建')
    }
    dialogOpen.value = false
    await load()
  } catch (e) {
    formError.value = e instanceof Error ? e.message : String(e)
  } finally {
    saving.value = false
  }
}

async function toggleDisabled(u: AdminUser) {
  const target = !u.disabled
  if (target && !window.confirm(`禁用「${u.username}」？其全部会话将立即失效。`)) return
  try {
    await setUserDisabled(u.id, target)
    toast(target ? '已禁用' : '已启用')
    await load()
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), 'error')
  }
}

async function remove(u: AdminUser) {
  if (!window.confirm(`删除用户「${u.username}」？其名下 ${u.monitor_count} 个监控与渠道将转移给你。`)) return
  try {
    const r = await deleteUser(u.id)
    toast(`已删除，资源已转移给 ${r.transferred_to}`)
    await load()
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), 'error')
  }
}

function toast(msg: string, color = 'success') {
  snackbarText.value = msg
  snackbarColor.value = color
  snackbar.value = true
}
</script>

<template>
  <div>
    <div class="d-flex align-center mb-4">
      <div class="text-h5">用户管理</div>
      <v-spacer />
      <v-text-field
        v-model="search" density="compact" hide-details clearable placeholder="搜索用户名/邮箱"
        prepend-inner-icon="mdi-magnify" max-width="260" class="me-2" @keyup.enter="load" @click:clear="load"
      />
      <v-btn color="primary" prepend-icon="mdi-plus" @click="openCreate">新建用户</v-btn>
    </div>

    <v-card>
      <v-table>
        <thead>
          <tr>
            <th>用户名</th>
            <th>邮箱</th>
            <th>角色</th>
            <th>来源</th>
            <th>监控数</th>
            <th>最后登录</th>
            <th>状态</th>
            <th class="text-end">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="u in items" :key="u.id">
            <td>
              <span class="font-weight-medium">{{ u.username }}</span>
              <span v-if="u.id === auth.user?.id" class="text-caption text-medium-emphasis ms-1">（我）</span>
              <div v-if="u.display_name" class="text-caption text-medium-emphasis">{{ u.display_name }}</div>
            </td>
            <td>{{ u.email || '—' }}</td>
            <td><v-chip size="small" :color="roleColor(u.role)">{{ roleTitle(u.role) }}</v-chip></td>
            <td>
              <v-chip v-if="u.oidc_provider" size="small" variant="tonal" prepend-icon="mdi-key-chain">{{ u.oidc_provider }}</v-chip>
              <span v-else class="text-medium-emphasis">本地</span>
            </td>
            <td>{{ u.monitor_count }}</td>
            <td class="text-caption">{{ u.last_login_at ? new Date(u.last_login_at).toLocaleString() : '从未' }}</td>
            <td>
              <v-chip size="small" :color="u.disabled ? 'default' : 'success'">{{ u.disabled ? '已禁用' : '正常' }}</v-chip>
            </td>
            <td class="text-end">
              <v-btn icon="mdi-pencil" size="small" variant="text" aria-label="编辑" @click="openEdit(u)" />
              <v-btn
                :icon="u.disabled ? 'mdi-account-check' : 'mdi-account-cancel'" size="small" variant="text"
                :color="u.disabled ? 'success' : 'warning'" :aria-label="u.disabled ? '启用' : '禁用'"
                :disabled="u.id === auth.user?.id" @click="toggleDisabled(u)"
              />
              <v-btn icon="mdi-delete" size="small" variant="text" color="error" aria-label="删除" :disabled="u.id === auth.user?.id" @click="remove(u)" />
            </td>
          </tr>
          <tr v-if="!items.length && !loading">
            <td colspan="8" class="text-center text-medium-emphasis py-6">暂无用户</td>
          </tr>
        </tbody>
      </v-table>
    </v-card>

    <v-dialog v-model="dialogOpen" max-width="520">
      <v-card class="pa-6">
        <div class="text-h6 mb-4">{{ editing ? `编辑 ${editing.username}` : '新建用户' }}</div>
        <v-form ref="form" @submit.prevent="save">
          <v-text-field v-model="uForm.username" label="用户名 *" :disabled="!!editing" :rules="[v => !!v || '必填']" class="mb-2" />
          <v-text-field
            v-model="uForm.password" label="密码" type="password"
            :rules="editing ? [] : [v => (v && v.length >= 8) || '至少 8 个字符']"
            :hint="editing ? '留空表示不修改；重置后该用户全部会话失效' : '至少 8 个字符'" persistent-hint class="mb-2"
          />
          <v-text-field v-model="uForm.email" label="邮箱" type="email" class="mb-2" />
          <v-text-field v-model="uForm.display_name" label="显示名" class="mb-2" />
          <v-select v-model="uForm.role" :items="roleItems" item-value="value" item-title="title" label="角色" :disabled="editing?.id === auth.user?.id" class="mb-2" @update:model-value="permsTouched = false" />
          <div class="text-subtitle-2 mb-1">允许的操作</div>
          <div class="text-caption text-medium-emphasis mb-1">{{ permsHint }}</div>
          <div v-if="uForm.role !== 'admin'" class="mb-2 d-flex flex-wrap ga-1">
            <v-checkbox
              v-for="p in permissionItems" :key="p.value"
              v-model="selectedPerms" :value="p.value" :label="p.title"
              density="compact" hide-details color="primary"
              @update:model-value="touchPerms"
            />
          </div>
          <v-alert v-if="formError" type="error" density="compact" class="mb-2">{{ formError }}</v-alert>
          <div class="d-flex justify-end ga-2 mt-2">
            <v-btn variant="text" @click="dialogOpen = false">取消</v-btn>
            <v-btn color="primary" type="submit" :loading="saving">保存</v-btn>
          </div>
        </v-form>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar" :color="snackbarColor" location="top right" :timeout="4000">
      {{ snackbarText }}
    </v-snackbar>
  </div>
</template>
