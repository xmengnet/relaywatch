<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { listOIDCProviders, getOIDCProvider, createOIDCProvider, updateOIDCProvider, deleteOIDCProvider, exportUrl, getOIDCCallbackBase, type OIDCProviderInfo } from '@/api/import'
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
const editingId = ref<number | null>(null)
const emptyProvider = () => ({
  name: '', issuer: '', client_id: '', client_secret: '', scopes: 'openid email profile', claim_rules: '', icon_url: '',
})
const pState = ref(emptyProvider())
const callbackBase = ref('')

const callbackURL = computed(() => `${callbackBase.value}/auth/oidc/${pState.value.name || '{名称}'}/callback`)

async function copyCallback() {
  try {
    await navigator.clipboard.writeText(callbackURL.value.replace('{名称}', ''));
    toast('回调地址已复制（名称为空时请自行补上）')
  } catch {
    toast('复制失败，请手动选择复制', 'error')
  }
}

const snackbar = ref(false)
const snackbarText = ref('')
const snackbarColor = ref('success')

onMounted(async () => {
  const [c, s, p] = await Promise.all([listChannels(), getSettings(), listOIDCProviders()])
  channels.value = c.items
  alertChannels.value = s.alert_channel_ids ?? []
  providers.value = p.items
  getOIDCCallbackBase().then(r => { callbackBase.value = r.base_url }).catch(() => {})
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
  editingId.value = null
  pState.value = emptyProvider()
  providerError.value = ''
  isSetupOpen.value = true
}

async function editProvider(p: OIDCProviderInfo) {
  if (!p.id) return
  try {
    const full = await getOIDCProvider(p.id)
    editingId.value = full.id ?? null
    pState.value = {
      name: full.name,
      issuer: full.issuer ?? '',
      client_id: full.client_id ?? '',
      client_secret: '', // 不回显，留空 = 不修改
      scopes: full.scopes ?? 'openid email profile',
      claim_rules: full.claim_rules ?? '',
      icon_url: full.icon_url ?? '',
    }
    providerError.value = ''
    isSetupOpen.value = true
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), 'error')
  }
}

async function saveProvider() {
  const { valid } = await pForm.value.validate()
  if (!valid) return
  savingProvider.value = true
  providerError.value = ''
  try {
    const body: Record<string, unknown> = { ...pState.value }
    if (body.claim_rules === '') body.claim_rules = undefined
    if (editingId.value) {
      // 编辑：secret 留空 = 不修改
      if (body.client_secret === '') delete body.client_secret
      await updateOIDCProvider(editingId.value, body)
      toast('Provider 已保存')
    } else {
      await createOIDCProvider(body)
      toast('Provider 已创建')
    }
    isSetupOpen.value = false
    providers.value = (await listOIDCProviders()).items
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
            <v-btn icon="mdi-pencil" size="small" variant="text" aria-label="编辑" @click="editProvider(p)" />
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
        <div class="text-h6 mb-4">{{ editingId ? '编辑 OIDC Provider' : '添加 OIDC Provider' }}</div>
        <v-form ref="pForm" @submit.prevent="saveProvider">
          <v-text-field v-model="pState.name" label="名称 *" hint="用于登录按钮与 URL，如 github、casdoor" persistent-hint :rules="[v => !!v || '必填']" class="mb-2" />
          <div v-if="callbackBase" class="text-caption text-medium-emphasis mb-4">
            回调地址（填到 IdP 的 Redirect URI）：
            <code class="text-none">{{ callbackURL }}</code>
            <v-btn size="x-small" variant="text" prepend-icon="mdi-content-copy" class="ms-1 text-none" @click="copyCallback">复制</v-btn>
          </div>
          <v-text-field v-model="pState.issuer" label="Issuer *" placeholder="https://accounts.google.com" hint="IdP 的 issuer 地址，服务端读 {issuer}/.well-known/openid-configuration 发现端点" persistent-hint :rules="[v => !!v || '必填']" class="mb-4" />
          <v-text-field v-model="pState.client_id" label="Client ID *" :rules="[v => !!v || '必填']" class="mb-4" />
          <v-text-field
            v-model="pState.client_secret" label="Client Secret" type="password"
            :rules="editingId ? [] : [v => !!v || '必填']"
            :hint="editingId ? '留空表示不修改' : '在 IdP 的应用配置里创建'" persistent-hint
            class="mb-4"
          />
          <v-text-field
            v-model="pState.icon_url" label="图标 URL（可选）"
            hint="登录按钮左侧显示；留空按名称自动匹配内置图标（github/google/gitlab 等），匹配不到用默认图标"
            persistent-hint
            class="mb-4"
          />
          <v-text-field
            v-model="pState.scopes" label="Scopes"
            hint="空格分隔，默认 openid email profile，一般不用改；仅当 IdP 要求特殊 scope（如 groups）时添加"
            persistent-hint
            class="mb-4"
          />
          <v-textarea
            v-model="pState.claim_rules" label="Claim 限制（可选）" rows="3" placeholder='{"email_domains":["example.com"]}'
            hint="JSON，如按邮箱域名限制可用的用户；留空不限制"
            persistent-hint
            class="mb-4"
          />
          <v-alert v-if="providerError" type="error" density="compact" class="mb-2">{{ providerError }}</v-alert>
          <div class="d-flex justify-end ga-2 mt-2">
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
