<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { listOIDCProviders } from '@/api/import'
import logoUrl from '@/assets/logo.svg'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { t } = useI18n()

const form = ref()
const username = ref('')
const password = ref('')
const loading = ref(false)
const errorMsg = ref('')
const oidcProviders = ref<Array<{ name: string; icon_url?: string }>>([])

// 注册（registration.enabled 开启时显示）
const isRegister = ref(false)
const regEmail = ref('')
const regForm = ref()

// 内置 Provider 图标：按 name 小写匹配（simple-icons CDN，非官方资源）。
// 加载失败（离线/内网）时回退为默认 mdi 图标，不影响登录。
const builtinIcons: Record<string, string> = {
  github: 'https://cdn.jsdelivr.net/npm/simple-icons@13/icons/github.svg',
  google: 'https://cdn.jsdelivr.net/npm/simple-icons@13/icons/google.svg',
  gitlab: 'https://cdn.jsdelivr.net/npm/simple-icons@13/icons/gitlab.svg',
  gitea: 'https://cdn.jsdelivr.net/npm/simple-icons@13/icons/gitea.svg',
  casdoor: 'https://cdn.jsdelivr.net/npm/simple-icons@13/icons/casdoor.svg',
  keycloak: 'https://cdn.jsdelivr.net/npm/simple-icons@13/icons/keycloak.svg',
  authentik: 'https://cdn.jsdelivr.net/npm/simple-icons@13/icons/authentik.svg',
  authelia: 'https://cdn.jsdelivr.net/npm/simple-icons@13/icons/authelia.svg',
  nextcloud: 'https://cdn.jsdelivr.net/npm/simple-icons@13/icons/nextcloud.svg',
  okta: 'https://cdn.jsdelivr.net/npm/simple-icons@13/icons/okta.svg',
  auth0: 'https://cdn.jsdelivr.net/npm/simple-icons@13/icons/auth0.svg',
}

const failedIcons = ref(new Set<string>())

function providerIcon(p: { name: string; icon_url?: string }): string {
  if (failedIcons.value.has(p.name)) return ''
  if (p.icon_url) return p.icon_url
  return builtinIcons[p.name.toLowerCase()] ?? ''
}

function onIconError(name: string) {
  failedIcons.value.add(name)
  failedIcons.value = new Set(failedIcons.value)
}

onMounted(async () => {
  if (route.query.error) {
    errorMsg.value = 'OIDC 登录失败，请重试或使用本地账户'
  }
  try {
    oidcProviders.value = (await listOIDCProviders()).items
  } catch {
    /* 未配置 OIDC */
  }
})

async function submit() {
  if (isRegister.value) {
    await submitRegister()
    return
  }
  const { valid } = await form.value.validate()
  if (!valid) return
  loading.value = true
  errorMsg.value = ''
  try {
    await auth.login(username.value, password.value)
    router.push((route.query.redirect as string) || '/')
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

async function submitRegister() {
  const { valid } = await regForm.value.validate()
  if (!valid) return
  loading.value = true
  errorMsg.value = ''
  try {
    await auth.register(username.value, password.value, regEmail.value || undefined)
    router.push('/')
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <v-card width="400" class="mx-auto pa-6">
    <div class="text-center mb-6">
      <img :src="logoUrl" alt="RelayWatch logo" width="56" height="56" class="rounded-lg mb-3" />
      <div class="text-h5 font-weight-bold">RelayWatch</div>
      <div class="text-body-2 text-medium-emphasis">版本更新监控</div>
    </div>

    <v-form v-if="!isRegister" ref="form" @submit.prevent="submit">
      <v-text-field
        v-model="username"
        :label="t('auth.username')"
        :rules="[v => !!v || '请输入用户名']"
        autocomplete="username"
        autofocus
      />
      <v-text-field
        v-model="password"
        :label="t('auth.password')"
        type="password"
        :rules="[v => !!v || '请输入密码']"
        autocomplete="current-password"
      />
      <v-alert v-if="errorMsg" type="error" density="compact" class="mb-4">{{ errorMsg }}</v-alert>
      <v-btn type="submit" block :loading="loading">{{ t('auth.login') }}</v-btn>
      <div v-if="auth.registrationOpen" class="text-center mt-3">
        <v-btn variant="text" size="small" color="primary" @click="isRegister = true; errorMsg = ''">
          没有账户？注册
        </v-btn>
      </div>
    </v-form>

    <v-form v-else ref="regForm" @submit.prevent="submit">
      <v-text-field
        v-model="username"
        label="用户名"
        :rules="[v => !!v || '请输入用户名']"
        autocomplete="username"
        autofocus
      />
      <v-text-field
        v-model="password"
        label="密码"
        type="password"
        :rules="[v => (v && v.length >= 8) || '至少 8 个字符']"
        autocomplete="new-password"
      />
      <v-text-field v-model="regEmail" label="邮箱（可选）" type="email" autocomplete="email" />
      <v-alert v-if="errorMsg" type="error" density="compact" class="mb-4">{{ errorMsg }}</v-alert>
      <v-btn type="submit" block color="primary" :loading="loading">注册</v-btn>
      <div class="text-center mt-3">
        <v-btn variant="text" size="small" @click="isRegister = false; errorMsg = ''">
          已有账户？登录
        </v-btn>
      </div>
    </v-form>

    <template v-if="oidcProviders.length">
      <div class="d-flex align-center my-4">
        <v-divider />
        <span class="text-caption text-medium-emphasis mx-2">或</span>
        <v-divider />
      </div>
      <v-btn
        v-for="p in oidcProviders"
        :key="p.name"
        block
        variant="outlined"
        color="grey-darken-1"
        class="oidc-btn mb-2"
        :href="`/auth/oidc/${p.name}`"
      >
        <template #prepend>
          <v-img
            v-if="providerIcon(p)"
            :src="providerIcon(p)"
            width="20"
            height="20"
            contain
            class="mr-2"
            :alt="p.name"
            @error="onIconError(p.name)"
          />
          <v-icon v-else icon="mdi-shield-account" size="20" class="mr-2" />
        </template>
        <span class="flex-grow-1 text-center">使用 {{ p.name }} 登录</span>
      </v-btn>
    </template>
  </v-card>
</template>

<style scoped>
/* 图二风格：白底、浅描边、图标在左、文字居中 */
.oidc-btn {
  background: #fff;
  text-transform: none;
  letter-spacing: normal;
  font-weight: 500;
}
.oidc-btn :deep(.v-btn__prepend) {
  position: absolute;
  left: 16px;
}
</style>
