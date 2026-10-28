<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useLocale } from '@/composables/useLocale'
import { useFormat } from '@/composables/useFormat'
import { getProfile, updateProfile, changePassword, listSessions, revokeSession, type SessionInfo } from '@/api/users'
import type { User } from '@/api/types'

const { t } = useI18n()
const { formatDateTime } = useFormat()
const auth = useAuthStore()
const { locale, setLocale, localeOptions } = useLocale()

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
    toast(t('profile.saved'))
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
    toast(t('profile.passwordChanged'))
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), 'error')
  } finally {
    savingPassword.value = false
  }
}

async function kick(sess: SessionInfo) {
  if (!window.confirm(sess.current ? t('profile.revokeCurrentConfirm') : t('profile.revokeConfirm'))) return
  revoking.value = sess.id
  try {
    await revokeSession(sess.id)
    if (sess.current) {
      await auth.logout()
      location.href = '/login'
      return
    }
    toast(t('profile.sessionRevoked'))
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
    <div class="text-h5 mb-4">{{ t('profile.title') }}</div>

    <v-card class="pa-6 mb-4" max-width="640">
      <div class="text-subtitle-1 font-weight-medium mb-2">{{ t('profile.accountInfo') }}</div>
      <div class="text-body-2 text-medium-emphasis mb-4">
        {{ t('profile.usernameRole', { username: user?.username, role: user ? t(`roles.${user.role}`) : '' }) }}
        <v-chip v-if="isOidc" size="small" variant="tonal" class="ms-2" prepend-icon="mdi-key-chain">{{ t('profile.oidcLogin') }}</v-chip>
      </div>
      <v-form ref="profileForm" @submit.prevent="saveProfile">
        <v-text-field v-model="displayName" :label="t('profile.displayName')" class="mb-2" />
        <v-text-field v-model="email" :label="t('profile.email')" type="email" class="mb-2" />
        <div class="d-flex justify-end">
          <v-btn color="primary" :loading="savingProfile" @click="saveProfile">{{ t('common.save') }}</v-btn>
        </div>
      </v-form>
    </v-card>

    <v-card class="pa-6 mb-4" max-width="640">
      <div class="text-subtitle-1 font-weight-medium mb-2">{{ t('common.language') }}</div>
      <div class="text-body-2 text-medium-emphasis mb-4">{{ t('profile.languageHint') }}</div>
      <v-select
        :model-value="locale"
        :items="localeOptions"
        item-title="title"
        item-value="value"
        :label="t('profile.languageLabel')"
        hide-details
        @update:model-value="setLocale"
      />
    </v-card>

    <v-card v-if="!isOidc" class="pa-6 mb-4" max-width="640">
      <div class="text-subtitle-1 font-weight-medium mb-2">{{ t('profile.changePassword') }}</div>
      <div class="text-body-2 text-medium-emphasis mb-4">{{ t('profile.passwordHint') }}</div>
      <v-form ref="pwForm" @submit.prevent="savePassword">
        <v-text-field v-model="oldPassword" :label="t('profile.currentPassword')" type="password" :rules="[v => !!v || t('profile.required')]" class="mb-2" />
        <v-text-field v-model="newPassword" :label="t('profile.newPassword')" type="password" :rules="[v => (v && v.length >= 8) || t('common.atLeast8')]" class="mb-2" />
        <v-text-field
          v-model="newPassword2" :label="t('profile.confirmNewPassword')" type="password"
          :rules="[v => v === newPassword || t('profile.passwordMismatch')]" class="mb-2"
        />
        <div class="d-flex justify-end">
          <v-btn color="primary" :loading="savingPassword" @click="savePassword">{{ t('profile.changePassword') }}</v-btn>
        </div>
      </v-form>
    </v-card>

    <v-alert v-else class="mb-4" max-width="640" type="info" variant="tonal">
      {{ t('profile.oidcPasswordHint') }}
    </v-alert>

    <v-card class="pa-6" max-width="640">
      <div class="text-subtitle-1 font-weight-medium mb-2">{{ t('profile.sessions') }}</div>
      <div class="text-body-2 text-medium-emphasis mb-4">{{ t('profile.sessionsHint') }}</div>
      <v-list v-if="sessions.length" density="compact">
        <v-list-item v-for="s in sessions" :key="s.id">
          <v-list-item-title class="d-flex align-center ga-2 text-body-2">
            <v-chip v-if="s.current" size="x-small" color="success" label>{{ t('common.current') }}</v-chip>
            <span class="text-truncate" style="max-width: 360px">{{ s.user_agent || t('common.unknownDevice') }}</span>
          </v-list-item-title>
          <v-list-item-subtitle class="text-caption">
            {{ s.ip || t('common.unknownIp') }} · {{ t('profile.loggedInAt', { time: formatDateTime(s.created_at) }) }}
          </v-list-item-subtitle>
          <template #append>
            <v-btn
              icon="mdi-logout" size="small" variant="text" color="warning"
              :aria-label="s.current ? t('auth.logout') : t('profile.revoke')" :loading="revoking === s.id" @click="kick(s)"
            />
          </template>
        </v-list-item>
      </v-list>
      <div v-else class="text-medium-emphasis py-2 text-body-2">{{ t('profile.noSessions') }}</div>
    </v-card>

    <v-snackbar v-model="snackbar" :color="snackbarColor" location="top right" :timeout="4000">
      {{ snackbarText }}
    </v-snackbar>
  </div>
</template>
