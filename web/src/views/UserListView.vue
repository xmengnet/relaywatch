<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useFormat } from '@/composables/useFormat'
import { listUsers, createUser, updateUser, setUserDisabled, deleteUser } from '@/api/users'
import type { AdminUser, Role } from '@/api/types'

const { t } = useI18n()
const { formatDateTime } = useFormat()
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

const roleItems = computed<Array<{ value: Role; title: string; color: string }>>(() => [
  { value: 'admin', title: t('roles.admin'), color: 'error' },
  { value: 'user', title: t('roles.user'), color: 'primary' },
  { value: 'viewer', title: t('roles.viewer'), color: 'info' },
])

// 细粒度权限项（与后端 store.AllPermissions 一致）
const permissionItems = computed(() => [
  { value: 'monitor.create', title: t('users.permMonitorCreate') },
  { value: 'channel.create', title: t('users.permChannelCreate') },
  { value: 'import', title: t('users.permImport') },
  { value: 'export', title: t('users.permExport') },
])
// 编辑时：null = 按角色默认；数组 = 显式配置
const permsTouched = ref(false)
const selectedPerms = ref<string[]>([])

const permsHint = computed(() => {
  if (uForm.value.role === 'admin') return t('users.permsHintAdmin')
  if (!permsTouched.value) {
    return uForm.value.role === 'viewer' ? t('users.permsHintViewerDefault') : t('users.permsHintUserDefault')
  }
  return t('users.permsHintExplicit')
})

const roleColor = (role: string) => roleItems.value.find((r) => r.value === role)?.color ?? 'default'
const roleTitle = (role: string) => roleItems.value.find((r) => r.value === role)?.title ?? role

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
      toast(t('users.userSaved'))
    } else {
      const p: { username: string; password: string; email: string; display_name: string; role: Role; permissions?: string[] } = { ...uForm.value }
      if (permsTouched.value && uForm.value.role !== 'admin') p.permissions = selectedPerms.value
      await createUser(p)
      toast(t('users.userCreated'))
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
  if (target && !window.confirm(t('users.disableConfirm', { name: u.username }))) return
  try {
    await setUserDisabled(u.id, target)
    toast(target ? t('users.disabledToast') : t('users.enabledToast'))
    await load()
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), 'error')
  }
}

async function remove(u: AdminUser) {
  if (!window.confirm(t('users.deleteConfirm', { name: u.username, n: u.monitor_count }))) return
  try {
    const r = await deleteUser(u.id)
    toast(t('users.deleted', { name: r.transferred_to }))
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
      <div class="text-h5">{{ t('users.title') }}</div>
      <v-spacer />
      <v-text-field
        v-model="search" density="compact" hide-details clearable :placeholder="t('users.search')"
        prepend-inner-icon="mdi-magnify" max-width="260" class="me-2" @keyup.enter="load" @click:clear="load"
      />
      <v-btn color="primary" prepend-icon="mdi-plus" @click="openCreate">{{ t('users.new') }}</v-btn>
    </div>

    <v-card>
      <v-table>
        <thead>
          <tr>
            <th>{{ t('users.headers.username') }}</th>
            <th>{{ t('users.headers.email') }}</th>
            <th>{{ t('users.headers.role') }}</th>
            <th>{{ t('users.headers.source') }}</th>
            <th>{{ t('users.headers.monitors') }}</th>
            <th>{{ t('users.headers.lastLogin') }}</th>
            <th>{{ t('users.headers.status') }}</th>
            <th class="text-end">{{ t('users.headers.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="u in items" :key="u.id">
            <td>
              <span class="font-weight-medium">{{ u.username }}</span>
              <span v-if="u.id === auth.user?.id" class="text-caption text-medium-emphasis ms-1">{{ t('users.me') }}</span>
              <div v-if="u.display_name" class="text-caption text-medium-emphasis">{{ u.display_name }}</div>
            </td>
            <td>{{ u.email || '—' }}</td>
            <td><v-chip size="small" :color="roleColor(u.role)">{{ roleTitle(u.role) }}</v-chip></td>
            <td>
              <v-chip v-if="u.oidc_provider" size="small" variant="tonal" prepend-icon="mdi-key-chain">{{ u.oidc_provider }}</v-chip>
              <span v-else class="text-medium-emphasis">{{ t('users.local') }}</span>
            </td>
            <td>{{ u.monitor_count }}</td>
            <td class="text-caption">{{ u.last_login_at ? formatDateTime(u.last_login_at) : t('users.lastLoginNever') }}</td>
            <td>
              <v-chip size="small" :color="u.disabled ? 'default' : 'success'">{{ u.disabled ? t('users.disabled') : t('users.active') }}</v-chip>
            </td>
            <td class="text-end">
              <v-btn icon="mdi-pencil" size="small" variant="text" :aria-label="t('common.edit')" @click="openEdit(u)" />
              <v-btn
                :icon="u.disabled ? 'mdi-account-check' : 'mdi-account-cancel'" size="small" variant="text"
                :color="u.disabled ? 'success' : 'warning'" :aria-label="u.disabled ? t('common.enabled') : t('common.disabled')"
                :disabled="u.id === auth.user?.id" @click="toggleDisabled(u)"
              />
              <v-btn icon="mdi-delete" size="small" variant="text" color="error" :aria-label="t('common.delete')" :disabled="u.id === auth.user?.id" @click="remove(u)" />
            </td>
          </tr>
          <tr v-if="!items.length && !loading">
            <td colspan="8" class="text-center text-medium-emphasis py-6">{{ t('users.noUsers') }}</td>
          </tr>
        </tbody>
      </v-table>
    </v-card>

    <v-dialog v-model="dialogOpen" max-width="520">
      <v-card class="pa-6">
        <div class="text-h6 mb-4">{{ editing ? t('users.dialogEdit', { username: editing.username }) : t('users.dialogNew') }}</div>
        <v-form ref="form" @submit.prevent="save">
          <v-text-field v-model="uForm.username" :label="t('users.username') + ' *'" :disabled="!!editing" :rules="[v => !!v || t('common.required')]" class="mb-2" />
          <v-text-field
            v-model="uForm.password" :label="t('users.password')" type="password"
            :rules="editing ? [] : [v => (v && v.length >= 8) || t('common.atLeast8')]"
            :hint="editing ? t('users.passwordHintEdit') : t('common.atLeast8')" persistent-hint class="mb-2"
          />
          <v-text-field v-model="uForm.email" :label="t('users.email')" type="email" class="mb-2" />
          <v-text-field v-model="uForm.display_name" :label="t('users.displayName')" class="mb-2" />
          <v-select v-model="uForm.role" :items="roleItems" item-value="value" item-title="title" :label="t('users.role')" :disabled="editing?.id === auth.user?.id" class="mb-2" @update:model-value="permsTouched = false" />
          <div class="text-subtitle-2 mb-1">{{ t('users.permissions') }}</div>
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
            <v-btn variant="text" @click="dialogOpen = false">{{ t('common.cancel') }}</v-btn>
            <v-btn color="primary" type="submit" :loading="saving">{{ t('common.save') }}</v-btn>
          </div>
        </v-form>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar" :color="snackbarColor" location="top right" :timeout="4000">
      {{ snackbarText }}
    </v-snackbar>
  </div>
</template>
