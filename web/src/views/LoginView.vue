<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { listOIDCProviders } from '@/api/import'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { t } = useI18n()

const form = ref()
const username = ref('')
const password = ref('')
const loading = ref(false)
const errorMsg = ref('')
const oidcProviders = ref<Array<{ name: string }>>([])

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
</script>

<template>
  <v-card width="400" class="mx-auto pa-6">
    <div class="text-center mb-6">
      <div class="text-h5 font-weight-bold">RelayWatch</div>
      <div class="text-body-2 text-medium-emphasis">版本更新监控</div>
    </div>

    <v-form ref="form" @submit.prevent="submit">
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
        variant="tonal"
        class="mb-2"
        :href="`/auth/oidc/${p.name}`"
      >
        使用 {{ p.name }} 登录
      </v-btn>
    </template>
  </v-card>
</template>
