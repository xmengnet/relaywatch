<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { listOIDCProviders, createOIDCProvider, deleteOIDCProvider, exportUrl, type OIDCProviderInfo } from '@/api/import'
import { getSettings, putSettings, listChannels } from '@/api/channels'

const { t } = useI18n()

// 告警渠道
const channels = ref<Array<{ id: number; name: string; type: string }>>([])
const alertChannels = ref<number[]>([])
const savingAlert = ref(false)

// OIDC（仅 admin 看得到写按钮，viewer 只读）
const providers = ref<OIDCProviderInfo[]>([])
const isSetupOpen = ref(false)
const savingProvider = ref(false)
const providerError = ref('')
const pForm = ref()
const pState = ref({
  name: '', issuer: '', client_id: '', client_secret: '', scopes: 'openid email profile', claim_rules: '',
})

const snackbar = ref(false)
const snackbarText = ref('')
const snackbarColor = ref('success')

onMounted(async () => {
  const [c, s, p] = await Promise.all([listChannels(), getSettings(), listOIDCProviders()])
  channels.value = c.items
  alertChannels.value = s.alert_channel_ids ?? []
  providers.value = p.items
})

async function saveAlert() {
  savingAlert.value = true
  try {
    await putSettings({ alert_channel_ids: alertChannels.value })
    toast('设置已保存')
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), 'error')
  } finally {
    savingAlert.value = false
  }
}

function openProvider() {
  pState.value = { name: '', issuer: '', client_id: '', client_secret: '', scopes: 'openid email profile', claim_rules: '' }
  providerError.value = ''
  isSetupOpen.value = true
}

async function saveProvider() {
  const { valid } = await pForm.value.validate()
  if (!valid) return
  savingProvider.value = true
  providerError.value = ''
  try {
    const body: Record<string, unknown> = { ...pState.value }
    if (body.claim_rules === '') body.claim_rules = undefined
    await createOIDCProvider(body)
    isSetupOpen.value = false
    providers.value = (await listOIDCProviders()).items
    toast('Provider 已创建')
  } catch (e) {
    providerError.value = e instanceof Error ? e.message : String(e)
  } finally {
    savingProvider.value = false
  }
}

async function removeProvider(p: OIDCProviderInfo) {
  if (!p.id || !window.confirm(`删除 Provider「${p.name}」？`)) return
  await deleteOIDCProvider(p.id)
  providers.value = (await listOIDCProviders()).items
}

function toast(msg: string, color = 'success') {
  snackbarText.value = msg
  snackbarColor.value = color
  snackbar.value = true
}

function downloadExport(redact: boolean) {
  // API 需要会话 Cookie，直接用 <a> 下载
  const a = document.createElement('a')
  a.href = exportUrl(redact)
  a.download = redact ? 'relaywatch-export.json' : 'relaywatch-export-full.json'
  a.click()
}
</script>

<template>
  <div>
    <div class="text-h5 mb-4">系统设置</div>

    <v-card class="pa-6 mb-4" max-width="860">
      <div class="text-subtitle-1 font-weight-medium mb-2">系统告警渠道</div>
      <div class="text-body-2 text-medium-emphasis mb-4">
        源不可用（连续检查失败）或上游消失（missing）时向这些渠道发告警。
      </div>
      <div v-if="!channels.length" class="text-medium-emphasis mb-4">还没有渠道，先到「渠道」页创建。</div>
      <v-checkbox
        v-for="c in channels"
        :key="c.id"
        v-model="alertChannels"
        :value="c.id"
        :label="`${c.name}（${c.type}）`"
        color="primary"
        density="comfortable"
        hide-details
      />
      <div class="d-flex justify-end mt-2">
        <v-btn color="primary" :loading="savingAlert" :disabled="!channels.length" @click="saveAlert">保存</v-btn>
      </div>
    </v-card>

    <v-card class="pa-6 mb-4" max-width="860">
      <div class="d-flex align-center mb-2">
        <div class="text-subtitle-1 font-weight-medium">OIDC 登录</div>
        <v-spacer />
        <v-btn size="small" color="primary" prepend-icon="mdi-plus" @click="openProvider">添加 Provider</v-btn>
      </div>
      <v-list v-if="providers.length" density="compact">
        <v-list-item v-for="p in providers" :key="p.id ?? p.name">
          <v-list-item-title class="font-weight-medium">{{ p.name }}</v-list-item-title>
          <v-list-item-subtitle>{{ p.issuer }}</v-list-item-subtitle>
          <template #append>
            <v-btn icon="mdi-delete" size="small" variant="text" color="error" aria-label="删除" @click="removeProvider(p)" />
          </template>
        </v-list-item>
      </v-list>
      <div v-else class="text-medium-emphasis py-2">未配置。添加后登录页会显示对应按钮。</div>
    </v-card>

    <v-card class="pa-6 mb-4" max-width="860">
      <div class="text-subtitle-1 font-weight-medium mb-2">导出</div>
      <div class="text-body-2 text-medium-emphasis mb-4">
        全部监控与渠道配置导出为 JSON。默认脱敏 token 等敏感字段；不脱敏的导出请妥善保管。
      </div>
      <div class="d-flex ga-2">
        <v-btn prepend-icon="mdi-download" @click="downloadExport(true)">导出（脱敏）</v-btn>
        <v-btn prepend-icon="mdi-download-box" variant="tonal" @click="downloadExport(false)">导出（含敏感字段）</v-btn>
      </div>
    </v-card>

    <v-card class="pa-6" max-width="860">
      <div class="text-subtitle-1 font-weight-medium mb-2">关于</div>
      <div class="text-body-2 text-medium-emphasis">
        {{ t('auth.logout') }} 等界面文案通过 i18n 管理；调度参数在 config.yaml 中调整。
      </div>
    </v-card>

    <v-dialog v-model="isSetupOpen" max-width="560">
      <v-card class="pa-6">
        <div class="text-h6 mb-4">添加 OIDC Provider</div>
        <v-form ref="pForm" @submit.prevent="saveProvider">
          <v-text-field v-model="pState.name" label="名称 *" hint="用于登录按钮与 URL，如 github" persistent-hint :rules="[v => !!v || '必填']" />
          <v-text-field v-model="pState.issuer" label="Issuer *" placeholder="https://accounts.google.com" :rules="[v => !!v || '必填']" />
          <v-text-field v-model="pState.client_id" label="Client ID *" :rules="[v => !!v || '必填']" />
          <v-text-field v-model="pState.client_secret" label="Client Secret *" type="password" :rules="[v => !!v || '必填']" />
          <v-text-field v-model="pState.scopes" label="Scopes" hint="默认 openid email profile" persistent-hint />
          <v-textarea v-model="pState.claim_rules" label="Claim 限制（可选）" rows="3" placeholder='{"email_domains":["example.com"]}' />
          <v-alert v-if="providerError" type="error" density="compact" class="mb-2">{{ providerError }}</v-alert>
          <div class="d-flex justify-end ga-2">
            <v-btn variant="text" @click="isSetupOpen = false">取消</v-btn>
            <v-btn color="primary" type="submit" :loading="savingProvider">保存</v-btn>
          </div>
        </v-form>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar" :color="snackbarColor" location="top right" :timeout="4000">
      {{ snackbarText }}
    </v-snackbar>
  </div>
</template>
