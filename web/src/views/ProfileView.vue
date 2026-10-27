<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { getProfile, updateProfile, changePassword, listSessions, revokeSession, type SessionInfo } from '@/api/users'
import type { User } from '@/api/types'

const auth = useAuthStore()

const user = ref<User | null>(null)
const isOidc = ref(false)
const email = ref('')
const displayName = ref('')
const savingProfile = ref(false)

const oldPassword = ref('')
const newPassword = ref('')
const newPassword2 = ref('')
const savingPassword = ref(false)
const pwForm = ref()
const profileForm = ref()

// 会话管理
const sessions = ref<SessionInfo[]>([])
const revoking = ref('')

const snackbar = ref(false)
const snackbarText = ref('')
const snackbarColor = ref('success')

function toast(msg: string, color = 'success') {
  snackbarText.value = msg
  snackbarColor.value = color
  snackbar.value = true
}

async function loadSessions() {
  try {
    sessions.value = (await listSessions()).items
  } catch {
    /* 忽略 */
  }
}

onMounted(async () => {
  const r = await getProfile()
  user.value = r.user
  isOidc.value = r.oidc
  email.value = r.user.email ?? ''
  displayName.value = r.user.display_name ?? ''
  await loadSessions()
})

async function saveProfile() {
  const { valid } = await profileForm.value.validate()
  if (!valid) return
  savingProfile.value = true
  try {
    const r = await updateProfile({ email: email.value, display_name: displayName.value })
    user.value = r.user
    auth.user = r.user
    toast('资料已保存')
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), 'error')
  } finally {
    savingProfile.value = false
  }
}

async function savePassword() {
  const { valid } = await pwForm.value.validate()
  if (!valid) return
  savingPassword.value = true
  try {
    await changePassword({ old_password: oldPassword.value, new_password: newPassword.value })
    oldPassword.value = newPassword.value = newPassword2.value = ''
    toast('密码已修改')
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), 'error')
  } finally {
    savingPassword.value = false
  }
}

async function kick(sess: SessionInfo) {
  if (!window.confirm(sess.current ? '吊销当前会话将立即登出，确认？' : '吊销该会话？')) return
  revoking.value = sess.id
  try {
    await revokeSession(sess.id)
    if (sess.current) {
      await auth.logout()
      location.href = '/login'
      return
    }
    toast('会话已吊销')
    await loadSessions()
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), 'error')
  } finally {
    revoking.value = ''
  }
}
</script>

<template>
  <div>
    <div class="text-h5 mb-4">个人资料</div>

    <v-card class="pa-6 mb-4" max-width="640">
      <div class="text-subtitle-1 font-weight-medium mb-2">账户信息</div>
      <div class="text-body-2 text-medium-emphasis mb-4">
        用户名 {{ user?.username }} · 角色 {{ user?.role }}
        <v-chip v-if="isOidc" size="small" variant="tonal" class="ms-2" prepend-icon="mdi-key-chain">OIDC 登录</v-chip>
      </div>
      <v-form ref="profileForm" @submit.prevent="saveProfile">
        <v-text-field v-model="displayName" label="显示名" class="mb-2" />
        <v-text-field v-model="email" label="邮箱" type="email" class="mb-2" />
        <div class="d-flex justify-end">
          <v-btn color="primary" :loading="savingProfile" @click="saveProfile">保存</v-btn>
        </div>
      </v-form>
    </v-card>

    <v-card v-if="!isOidc" class="pa-6 mb-4" max-width="640">
      <div class="text-subtitle-1 font-weight-medium mb-2">修改密码</div>
      <div class="text-body-2 text-medium-emphasis mb-4">修改成功后请重新登录其他设备；当前会话保持有效。</div>
      <v-form ref="pwForm" @submit.prevent="savePassword">
        <v-text-field v-model="oldPassword" label="当前密码" type="password" :rules="[v => !!v || '必填']" class="mb-2" />
        <v-text-field v-model="newPassword" label="新密码" type="password" :rules="[v => (v && v.length >= 8) || '至少 8 个字符']" class="mb-2" />
        <v-text-field
          v-model="newPassword2" label="确认新密码" type="password"
          :rules="[v => v === newPassword || '两次输入不一致']" class="mb-2"
        />
        <div class="d-flex justify-end">
          <v-btn color="primary" :loading="savingPassword" @click="savePassword">修改密码</v-btn>
        </div>
      </v-form>
    </v-card>

    <v-alert v-else class="mb-4" max-width="640" type="info" variant="tonal">
      当前账户通过 OIDC 登录，密码由身份提供方管理。
    </v-alert>

    <v-card class="pa-6" max-width="640">
      <div class="text-subtitle-1 font-weight-medium mb-2">登录会话</div>
      <div class="text-body-2 text-medium-emphasis mb-4">当前账户的全部有效会话；发现陌生设备可立即吊销。</div>
      <v-list v-if="sessions.length" density="compact">
        <v-list-item v-for="s in sessions" :key="s.id">
          <v-list-item-title class="d-flex align-center ga-2 text-body-2">
            <v-chip v-if="s.current" size="x-small" color="success" label>当前</v-chip>
            <span class="text-truncate" style="max-width: 360px">{{ s.user_agent || '未知设备' }}</span>
          </v-list-item-title>
          <v-list-item-subtitle class="text-caption">
            {{ s.ip || '未知 IP' }} · 登录于 {{ new Date(s.created_at).toLocaleString() }}
          </v-list-item-subtitle>
          <template #append>
            <v-btn
              icon="mdi-logout" size="small" variant="text" color="warning"
              :aria-label="s.current ? '登出' : '吊销会话'" :loading="revoking === s.id" @click="kick(s)"
            />
          </template>
        </v-list-item>
      </v-list>
      <div v-else class="text-medium-emphasis py-2 text-body-2">无有效会话</div>
    </v-card>

    <v-snackbar v-model="snackbar" :color="snackbarColor" location="top right" :timeout="4000">
      {{ snackbarText }}
    </v-snackbar>
  </div>
</template>
