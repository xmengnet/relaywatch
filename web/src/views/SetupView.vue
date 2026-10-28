<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const auth = useAuthStore()
const { t } = useI18n()

const form = ref()
const username = ref('')
const password = ref('')
const confirm = ref('')
const loading = ref(false)
const errorMsg = ref('')

async function submit() {
  const { valid } = await form.value.validate()
  if (!valid) return
  if (password.value !== confirm.value) {
    errorMsg.value = t('setup.passwordMismatch')
    return
  }
  loading.value = true
  errorMsg.value = ''
  try {
    await auth.setup(username.value, password.value)
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
      <div class="text-h5 font-weight-bold">{{ t('setup.welcome') }}</div>
      <div class="text-body-2 text-medium-emphasis">{{ t('setup.subtitle') }}</div>
    </div>

    <v-form ref="form" @submit.prevent="submit">
      <v-text-field v-model="username" :label="t('auth.username')" :rules="[v => !!v || t('auth.usernameRequired')]" autocomplete="username" autofocus />
      <v-text-field v-model="password" :label="t('auth.password')" type="password" :rules="[v => (v && v.length >= 8) || t('common.atLeast8')]" autocomplete="new-password" />
      <v-text-field v-model="confirm" :label="t('setup.confirmPassword')" type="password" :rules="[v => !!v || t('setup.confirmRequired')]" autocomplete="new-password" />
      <v-alert v-if="errorMsg" type="error" density="compact" class="mb-4">{{ errorMsg }}</v-alert>
      <v-btn type="submit" block :loading="loading">{{ t('setup.createAccount') }}</v-btn>
    </v-form>
  </v-card>
</template>
