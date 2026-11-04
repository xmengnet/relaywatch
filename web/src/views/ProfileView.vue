<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useLocale } from '@/composables/useLocale'
import { useFormat } from '@/composables/useFormat'
import { getProfile, updateProfile, changePassword, listSessions, revokeSession, unbindOIDC, getBindPending, confirmOIDCBind, type SessionInfo } from '@/api/users'
import { listAPITokens, createAPIToken, revokeAPIToken, type APIToken } from '@/api/tokens'
import { listOIDCProviders } from '@/api/import'
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

// API Tokens
const apiTokens = ref<APIToken[]>([])
const tokenDialog = ref(false)
const newTokenName = ref('')
const newTokenDays = ref(30)
const creatingToken = ref(false)
const createdTokenDialog = ref(false)
const createdTokenSecret = ref('')
const revokingTokenId = ref<number | null>(null)

const expireOptions = [
  { title: t('profile.tokenExpires30d'), value: 30 },
  { title: t('profile.tokenExpires7d'), value: 7 },
  { title: t('profile.tokenExpires90d'), value: 90 },
  { title: t('profile.tokenExpiresNever'), value: 0 },
]

async function loadTokens() {
  try {
    apiTokens.value = (await listAPITokens()).items
  } catch {
    /* 忽略 */
  }
}

function openNewTokenDialog() {
  newTokenName.value = ''
  newTokenDays.value = 30
  tokenDialog.value = true
}

async function handleCreateToken() {
  if (!newTokenName.value.trim()) return
  creatingToken.value = true
  try {
    let expires_at: string | undefined
    if (newTokenDays.value > 0) {
      expires_at = new Date(Date.now() + newTokenDays.value * 86400000).toISOString()
    }
    const res = await createAPIToken({ name: newTokenName.value.trim(), expires_at })
    createdTokenSecret.value = res.token
    tokenDialog.value = false
    createdTokenDialog.value = true
    await loadTokens()
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), 'error')
  } finally {
    creatingToken.value = false
  }
}

async function handleRevokeToken(tok: APIToken) {
  if (!window.confirm(t('profile.tokenRevokeConfirm', { name: tok.name }))) return
  revokingTokenId.value = tok.id
  try {
    await revokeAPIToken(tok.id)
    toast(t('profile.tokenRevoked'))
    await loadTokens()
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), 'error')
  } finally {
    revokingTokenId.value = null
  }
}

async function copyCreatedToken() {
  try {
    await navigator.clipboard.writeText(createdTokenSecret.value)
    toast(t('profile.tokenCopied'))
  } catch {
    toast(t('common.copyFailed'), 'error')
  }
}

// OIDC 绑定
const oidcProvider = ref('')
const hasPassword = ref(false)
const idpList = ref<Array<{ name: string; icon_url?: string }>>([])

// 绑定确认：IdP 回调只登记待确认项，回跳后由当前会话用户确认生效
const bindConfirmInfo = ref<{ provider: string; identity: string } | null>(null)
const bindConfirmToken = ref('')
const confirmingBind = ref(false)

async function consumeBindConfirm() {
  const q = new URLSearchParams(location.search)
  const token = q.get('bind_confirm')
  if (!token) return
  history.replaceState({}, '', location.pathname)
  try {
    bindConfirmInfo.value = await getBindPending(token)
    bindConfirmToken.value = token
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), 'error')
  }
}

async function confirmBind() {
  confirmingBind.value = true
  try {
    await confirmOIDCBind(bindConfirmToken.value)
    toast(t('profile.oidcBindDone'))
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), 'error')
  } finally {
    confirmingBind.value = false
    bindConfirmInfo.value = null
    bindConfirmToken.value = ''
    // 刷新绑定状态
    try {
      const r = await getProfile()
      isOidc.value = r.oidc
      oidcProvider.value = r.oidc_provider ?? ''
      hasPassword.value = r.has_password ?? true
    } catch { /* 忽略 */ }
  }
}

function cancelBind() {
  bindConfirmInfo.value = null
  bindConfirmToken.value = ''
}

// 绑定流程经 IdP 302 回跳，带查询参数回报结果
function consumeOidcQuery() {
  const q = new URLSearchParams(location.search)
  if (q.get('oidc_bound')) toast(t('profile.oidcBindDone'))
  else if (q.get('oidc_error') === 'bind_taken') toast(t('profile.oidcBindTaken'), 'error')
  else if (q.get('oidc_error')) toast(t('profile.oidcBindFailed'), 'error')
  else return
  history.replaceState({}, '', location.pathname)
}

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
  oidcProvider.value = r.oidc_provider ?? ''
  hasPassword.value = r.has_password ?? true
  email.value = r.user.email ?? ''
  displayName.value = r.user.display_name ?? ''
  consumeOidcQuery()
  consumeBindConfirm()
  try {
    // 登录页同款匿名列表：只含启用的 Provider（name/icon_url），够渲染绑定按钮
    idpList.value = (await listOIDCProviders()).items
  } catch {
    /* 匿名列表不可用时仅隐藏绑定入口 */
  }
  await loadSessions()
  await loadTokens()
})

function bindProvider(name: string) {
  location.href = `/auth/oidc/${encodeURIComponent(name)}/bind`
}

async function unbind() {
  if (!window.confirm(t('profile.oidcUnbindConfirm', { name: oidcProvider.value }))) return
  try {
    await unbindOIDC()
    oidcProvider.value = ''
    isOidc.value = false
    toast(t('profile.oidcUnbindDone'))
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), 'error')
  }
}

async function saveProfile() {
  if (savingProfile.value) return // Enter 可绕过按钮 loading 拦截
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
  if (savingPassword.value) return
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
        <v-text-field v-model="email" :label="t('profile.email')" type="email" class="mb-2"
          :rules="[v => !v || /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(v) || t('profile.invalidEmail')]"
        />
        <div class="d-flex justify-end">
          <v-btn color="primary" :loading="savingProfile" @click="saveProfile">{{ t('common.save') }}</v-btn>
        </div>
      </v-form>
    </v-card>

    <v-card class="pa-6 mb-4" max-width="640">
      <div class="text-subtitle-1 font-weight-medium mb-2">{{ t('profile.oidcBindTitle') }}</div>
      <div class="text-body-2 text-medium-emphasis mb-4">{{ t('profile.oidcBindHint') }}</div>
      <div v-if="oidcProvider" class="d-flex align-center ga-2">
        <v-chip prepend-icon="mdi-key-chain" variant="tonal">{{ t('profile.oidcBoundTo', { name: oidcProvider }) }}</v-chip>
        <v-spacer />
        <v-btn variant="text" color="error" :disabled="!hasPassword" @click="unbind">{{ t('profile.oidcUnbind') }}</v-btn>
      </div>
      <div v-else class="d-flex ga-2 flex-wrap align-center">
        <v-btn
          v-for="p in idpList" :key="p.name" variant="tonal" color="primary"
          prepend-icon="mdi-link-variant" @click="bindProvider(p.name)"
        >{{ t('profile.oidcBindTo', { name: p.name }) }}</v-btn>
        <span v-if="!idpList.length" class="text-body-2 text-medium-emphasis">{{ t('profile.oidcNoProviders') }}</span>
      </div>
      <v-alert v-if="oidcProvider && !hasPassword" type="info" density="compact" variant="tonal" class="mt-3">
        {{ t('profile.oidcUnbindNeedPassword') }}
      </v-alert>
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

    <v-card class="pa-6 mb-4" max-width="640">
      <div class="text-subtitle-1 font-weight-medium mb-2">
        {{ hasPassword ? t('profile.changePassword') : t('profile.setPassword') }}
      </div>
      <div class="text-body-2 text-medium-emphasis mb-4">
        {{ hasPassword ? t('profile.passwordHint') : (isOidc ? t('profile.setPasswordOidcHint') : t('profile.passwordHint')) }}
      </div>
      <v-form ref="pwForm" @submit.prevent="savePassword">
        <v-text-field
          v-if="hasPassword" v-model="oldPassword" :label="t('profile.currentPassword')"
          type="password" :rules="[v => !!v || t('profile.required')]" class="mb-2"
        />
        <v-text-field v-model="newPassword" :label="t('profile.newPassword')" type="password" :rules="[v => (v && v.length >= 8) || t('common.atLeast8')]" class="mb-2" />
        <v-text-field
          v-model="newPassword2" :label="t('profile.confirmNewPassword')" type="password"
          :rules="[v => v === newPassword || t('profile.passwordMismatch')]" class="mb-2"
        />
        <div class="d-flex justify-end">
          <v-btn color="primary" :loading="savingPassword" @click="savePassword">
            {{ hasPassword ? t('profile.changePassword') : t('profile.setPassword') }}
          </v-btn>
        </div>
      </v-form>
    </v-card>

    <v-card class="pa-6 mb-4" max-width="640">
      <div class="d-flex align-center justify-space-between mb-2">
        <div class="text-subtitle-1 font-weight-medium">{{ t('profile.apiTokens') }}</div>
        <v-btn
          color="primary"
          size="small"
          variant="tonal"
          prepend-icon="mdi-plus"
          @click="openNewTokenDialog"
        >
          {{ t('profile.newToken') }}
        </v-btn>
      </div>
      <div class="text-body-2 text-medium-emphasis mb-3 d-flex align-center justify-space-between">
        <span>{{ t('profile.apiTokensHint') }}</span>
        <router-link to="/help?tab=api" class="text-decoration-none text-caption text-primary text-no-wrap ms-2">
          {{ t('profile.viewApiDocs') }}
        </router-link>
      </div>

      <v-table v-if="apiTokens.length" density="compact">
        <thead>
          <tr>
            <th>{{ t('profile.tokenName') }}</th>
            <th>{{ t('profile.tokenExpiresAt') }}</th>
            <th>{{ t('profile.tokenLastUsed') }}</th>
            <th class="text-end">{{ t('common.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="tok in apiTokens" :key="tok.id">
            <td class="font-weight-medium"><code>{{ tok.name }}</code></td>
            <td class="text-caption">
              <span v-if="tok.expires_at">{{ formatDateTime(tok.expires_at) }}</span>
              <span v-else class="text-medium-emphasis">{{ t('profile.tokenExpiresNever') }}</span>
            </td>
            <td class="text-caption text-medium-emphasis">
              <span v-if="tok.last_used_at">{{ formatDateTime(tok.last_used_at) }}</span>
              <span v-else>{{ t('profile.tokenLastUsedNever') }}</span>
            </td>
            <td class="text-end">
              <v-btn
                icon="mdi-delete-outline"
                size="small"
                variant="text"
                color="error"
                :loading="revokingTokenId === tok.id"
                @click="handleRevokeToken(tok)"
              />
            </td>
          </tr>
        </tbody>
      </v-table>
      <div v-else class="text-medium-emphasis py-2 text-body-2">{{ t('profile.noTokens') }}</div>
    </v-card>

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

    <v-dialog :model-value="bindConfirmInfo !== null" persistent max-width="440">
      <v-card class="pa-4" v-if="bindConfirmInfo">
        <div class="text-subtitle-1 font-weight-medium mb-2">{{ t('profile.oidcConfirmTitle') }}</div>
        <div class="text-body-2 text-medium-emphasis mb-4">
          {{ t('profile.oidcConfirmText', { provider: bindConfirmInfo.provider, identity: bindConfirmInfo.identity }) }}
        </div>
        <div class="d-flex justify-end ga-2">
          <v-btn variant="text" :disabled="confirmingBind" @click="cancelBind">{{ t('common.cancel') }}</v-btn>
          <v-btn color="primary" :loading="confirmingBind" @click="confirmBind">{{ t('profile.oidcConfirmOk') }}</v-btn>
        </div>
      </v-card>
    </v-dialog>

    <!-- 创建 Token 对话框 -->
    <v-dialog v-model="tokenDialog" max-width="480">
      <v-card class="pa-4">
        <v-card-title class="text-subtitle-1 font-weight-medium px-2">{{ t('profile.newToken') }}</v-card-title>
        <v-card-text class="px-2 pt-2">
          <v-text-field
            v-model="newTokenName"
            :label="t('profile.tokenName')"
            :placeholder="t('profile.tokenNamePlaceholder')"
            class="mb-3"
            autofocus
            @keydown.enter="handleCreateToken"
          />
          <v-select
            v-model="newTokenDays"
            :items="expireOptions"
            item-title="title"
            item-value="value"
            :label="t('profile.tokenExpires')"
          />
        </v-card-text>
        <v-card-actions class="justify-end px-2 pb-0">
          <v-btn variant="text" @click="tokenDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn
            color="primary"
            :loading="creatingToken"
            :disabled="!newTokenName.trim()"
            @click="handleCreateToken"
          >
            {{ t('common.confirm') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 展示新生成 Token 对话框 (一次性) -->
    <v-dialog v-model="createdTokenDialog" max-width="520" persistent>
      <v-card class="pa-4">
        <v-card-title class="d-flex align-center ga-2 text-success px-2">
          <v-icon color="success">mdi-check-circle</v-icon>
          {{ t('profile.tokenCreatedTitle') }}
        </v-card-title>
        <v-card-text class="px-2 pt-2">
          <v-alert type="warning" variant="tonal" density="compact" class="mb-4">
            {{ t('profile.tokenCreatedWarn') }}
          </v-alert>
          <div class="pa-3 rounded d-flex align-center justify-space-between border text-body-2" style="word-break: break-all; background-color: rgba(var(--v-theme-on-surface), 0.05);">
            <code>{{ createdTokenSecret }}</code>
            <v-btn
              icon="mdi-content-copy"
              size="small"
              variant="text"
              color="primary"
              class="ms-2"
              @click="copyCreatedToken"
            />
          </div>
        </v-card-text>
        <v-card-actions class="justify-end px-2 pb-0">
          <v-btn color="primary" @click="createdTokenDialog = false; createdTokenSecret = ''">
            {{ t('common.close') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>
