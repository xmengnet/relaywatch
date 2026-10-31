<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getSiteConfig, putSiteConfig, type MirrorRule, type HostDefault } from '@/api/site'
import { getSettings, putSettings, listChannels } from '@/api/channels'
import { getMailConfig, putMailConfig, sendTestMail, type MailConfig } from '@/api/mail'
import { listAllOIDCProviders, getOIDCProvider, createOIDCProvider, updateOIDCProvider, deleteOIDCProvider, getOIDCCallbackBase, type OIDCProviderInfo } from '@/api/import'
import SiteMirrorEditor from '@/components/SiteMirrorEditor.vue'

const { t } = useI18n()

const loading = ref(true)
const saving = ref(false)
const snackbar = ref(false)
const snackbarText = ref('')
const snackbarColor = ref('success')

// API 镜像/代理规则
const mirrors = ref<MirrorRule[]>([])

// 源站点默认值（仅常用的自建服务类型）
const hostDefs = ref<HostDefault[]>([])
const hostDefTypes = computed(() => [
  { value: 'gitea_release', title: t('site.hostTypeGitea') },
  { value: 'gitlab_release', title: t('site.hostTypeGitlab') },
])

// 系统告警渠道（自「设置」页迁入）
const channels = ref<Array<{ id: number; name: string; type: string }>>([])
const alertChannels = ref<number[]>([])
const savingAlert = ref(false)

// 注册开关（自「设置」页迁入）
const registrationEnabled = ref(false)
const registrationRole = ref('user')
const registrationNeedEmail = ref(false)
const savingReg = ref(false)

// 全局邮件配置（注册验证码发信用）
const mail = ref<MailConfig>({ host: '', port: '587', username: '', password: '', from: '', encryption: 'starttls' })
const mailPasswordSet = ref(false)
const savingMail = ref(false)
const sendingTestMail = ref(false)

// OIDC 登录（自「设置」页迁入）
const providers = ref<OIDCProviderInfo[]>([])
const isSetupOpen = ref(false)
const savingProvider = ref(false)
const providerError = ref('')
const pForm = ref()
const editingId = ref<number | null>(null)
const emptyProvider = () => ({
  name: '', issuer: '', client_id: '', client_secret: '', scopes: 'openid email profile', claim_rules: '', icon_url: '', enabled: true,
})
const pState = ref(emptyProvider())
const callbackBase = ref('')
const callbackURL = computed(() => `${callbackBase.value}/auth/oidc/${pState.value.name || '{name}'}/callback`)

const encryptionOptions = computed(() => [
  { value: 'starttls', title: 'STARTTLS' },
  { value: 'ssl', title: 'SSL/TLS' },
  { value: 'none', title: t('site.mailNone') },
])

const registrationRoleOptions = computed(() => [
  { value: 'user', title: t('settings.roleUserDesc') },
  { value: 'viewer', title: t('settings.roleViewerDesc') },
])

function toast(msg: string, color = 'success') {
  snackbarText.value = msg
  snackbarColor.value = color
  snackbar.value = true
}

function hostDefOf(type: string): string {
  return hostDefs.value.find((d) => d.source_type === type)?.host ?? ''
}

function setHostDef(type: string, host: string) {
  const i = hostDefs.value.findIndex((d) => d.source_type === type)
  if (i >= 0) {
    if (host) hostDefs.value[i].host = host
    else hostDefs.value.splice(i, 1)
  } else if (host) {
    hostDefs.value.push({ source_type: type, host })
  }
}

onMounted(async () => {
  try {
    const [cfg, s, p, m] = await Promise.all([getSiteConfig(), getSettings(), listAllOIDCProviders(), getMailConfig().catch(() => null)])
    mirrors.value = cfg.mirrors ?? []
    hostDefs.value = cfg.host_defaults ?? []
    const chans = await listChannels()
    channels.value = chans.items
    alertChannels.value = s.alert_channel_ids ?? []
    registrationEnabled.value = s.registration_enabled ?? false
    registrationRole.value = s.registration_default_role || 'user'
    registrationNeedEmail.value = s.registration_require_email ?? false
    if (m) {
      mail.value = { ...mail.value, ...m.config, password: '' }
      mailPasswordSet.value = m.password_set
    }
    providers.value = p.items
    getOIDCCallbackBase().then(r => { callbackBase.value = r.base_url }).catch(() => {})
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), 'error')
  } finally {
    loading.value = false
  }
})

function addMirror() {
  mirrors.value.push({ from: '', to: '' })
}

async function save() {
  saving.value = true
  try {
    await putSiteConfig({
      mirrors: mirrors.value.filter((m) => m.from.trim() && m.to.trim()),
      host_defaults: hostDefs.value,
    })
    toast(t('site.siteSaved'))
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), 'error')
  } finally {
    saving.value = false
  }
}

async function saveRegistration() {
  savingReg.value = true
  try {
    await putSettings({
      registration_enabled: registrationEnabled.value,
      registration_default_role: registrationRole.value,
      registration_require_email: registrationNeedEmail.value,
    })
    toast(t('settings.registrationSaved'))
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), 'error')
  } finally {
    savingReg.value = false
  }
}

async function saveMail() {
  savingMail.value = true
  try {
    const p: Record<string, unknown> = { ...mail.value }
    if (!p.password) delete p.password // 留空 = 不修改
    await putMailConfig(p)
    toast(t('site.mailSaved'))
    const m = await getMailConfig()
    mailPasswordSet.value = m.password_set
    mail.value.password = ''
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), 'error')
  } finally {
    savingMail.value = false
  }
}

async function testMail() {
  sendingTestMail.value = true
  try {
    const r = await sendTestMail()
    if (r.ok) toast(t('site.mailTestSent'))
    else toast(r.error || t('site.mailSendFailed'), 'error')
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), 'error')
  } finally {
    sendingTestMail.value = false
  }
}

async function saveAlert() {
  savingAlert.value = true
  try {
    await putSettings({ alert_channel_ids: alertChannels.value })
    toast(t('site.alertSaved'))
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
      enabled: full.enabled ?? true,
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
      if (body.client_secret === '') delete body.client_secret
      await updateOIDCProvider(editingId.value, body)
      toast(t('site.providerSaved'))
    } else {
      await createOIDCProvider(body)
      toast(t('site.providerCreated'))
    }
    isSetupOpen.value = false
    providers.value = (await listAllOIDCProviders()).items
  } catch (e) {
    providerError.value = e instanceof Error ? e.message : String(e)
  } finally {
    savingProvider.value = false
  }
}

async function removeProvider(p: OIDCProviderInfo) {
  if (!p.id || !window.confirm(t('site.deleteProviderConfirm', { name: p.name }))) return
  await deleteOIDCProvider(p.id)
  providers.value = (await listAllOIDCProviders()).items
}

async function copyCallback() {
  try {
    await navigator.clipboard.writeText(callbackURL.value.replace('{name}', ''))
    toast(t('site.callbackCopied'))
  } catch {
    toast(t('site.copyFailed'), 'error')
  }
}
</script>

<template>
  <div>
    <div class="text-h5 mb-1">{{ t('site.title') }}</div>
    <div class="text-body-2 text-medium-emphasis mb-4">{{ t('site.subtitle') }}</div>

    <v-card class="pa-6 mb-4" max-width="860" :loading="loading">
      <div class="text-subtitle-1 font-weight-medium mb-1">{{ t('site.mirrors') }}</div>
      <div class="text-body-2 text-medium-emphasis mb-4">{{ t('site.mirrorsDesc') }}</div>
      <SiteMirrorEditor v-model="mirrors" />
      <div v-if="mirrors.length === 0" class="text-medium-emphasis mb-4 text-body-2">{{ t('site.mirrorsEmpty') }}</div>
      <v-btn size="small" variant="tonal" prepend-icon="mdi-plus" @click="addMirror">{{ t('site.addRule') }}</v-btn>
    </v-card>

    <v-card class="pa-6 mb-4" max-width="860" :loading="loading">
      <div class="text-subtitle-1 font-weight-medium mb-1">{{ t('site.hostDefaults') }}</div>
      <div class="text-body-2 text-medium-emphasis mb-4">{{ t('site.hostDefaultsDesc') }}</div>
      <v-text-field
        v-for="ht in hostDefTypes" :key="ht.value"
        :model-value="hostDefOf(ht.value)" :label="ht.title"
        :placeholder="t('site.hostDefaultPlaceholder')"
        hide-details density="compact" class="mb-2"
        @update:model-value="v => setHostDef(ht.value, v)"
      />
    </v-card>

    <v-card class="pa-6 mb-4" max-width="860" :loading="loading">
      <div class="text-subtitle-1 font-weight-medium mb-2">{{ t('site.alertChannels') }}</div>
      <div class="text-body-2 text-medium-emphasis mb-4">{{ t('site.alertChannelsDesc') }}</div>
      <div v-if="!channels.length" class="text-medium-emphasis mb-4">{{ t('site.noChannels') }}</div>
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
        <v-btn color="primary" :loading="savingAlert" :disabled="!channels.length" @click="saveAlert">{{ t('common.save') }}</v-btn>
      </div>
    </v-card>

    <v-card class="pa-6 mb-4" max-width="860" :loading="loading">
      <div class="text-subtitle-1 font-weight-medium mb-2">{{ t('settings.registration') }}</div>
      <div class="text-body-2 text-medium-emphasis mb-4">{{ t('settings.registrationDesc') }}</div>
      <v-switch
        v-model="registrationEnabled" color="primary" density="compact" hide-details
        :label="registrationEnabled ? t('settings.registrationOn') : t('settings.registrationOff')" class="mb-2"
      />
      <v-select
        v-model="registrationRole" :items="registrationRoleOptions"
        item-value="value" item-title="title" :label="t('settings.registrationRole')" max-width="360" class="mb-2" :disabled="!registrationEnabled"
      />
      <v-switch
        v-model="registrationNeedEmail" color="primary" density="compact" hide-details :label="t('site.registrationNeedEmail')"
        class="mb-2" :disabled="!registrationEnabled"
      />
      <div class="d-flex justify-end">
        <v-btn color="primary" :loading="savingReg" @click="saveRegistration">{{ t('common.save') }}</v-btn>
      </div>
    </v-card>

    <v-card class="pa-6 mb-4" max-width="860" :loading="loading">
      <div class="text-subtitle-1 font-weight-medium mb-2">{{ t('site.mail') }}</div>
      <div class="text-body-2 text-medium-emphasis mb-4">{{ t('site.mailDesc') }}</div>
      <div class="d-flex ga-2 mb-2">
        <v-text-field v-model="mail.host" :label="t('site.mailHost') + ' *'" density="compact" class="flex-grow-1" />
        <v-text-field v-model="mail.port" :label="t('site.mailPort') + ' *'" density="compact" style="max-width: 110px" />
        <v-select
          v-model="mail.encryption" :items="encryptionOptions"
          item-value="value" item-title="title" :label="t('site.mailEncryption')" density="compact" style="max-width: 140px"
        />
      </div>
      <div class="d-flex ga-2 mb-2">
        <v-text-field v-model="mail.username" :label="t('site.mailUsername')" density="compact" class="flex-grow-1" />
        <v-text-field
          v-model="mail.password" :label="t('site.mailPassword')" type="password" density="compact" class="flex-grow-1"
          :placeholder="mailPasswordSet ? t('site.mailPasswordSet') : ''"
        />
      </div>
      <v-text-field v-model="mail.from" :label="t('site.mailFrom') + ' *'" placeholder="relaywatch@example.com" density="compact" class="mb-2" />
      <div class="d-flex justify-end ga-2">
        <v-btn :loading="sendingTestMail" :disabled="!mail.host || !mail.from" @click="testMail">{{ t('site.mailTest') }}</v-btn>
        <v-btn color="primary" :loading="savingMail" :disabled="!mail.host || !mail.from" @click="saveMail">{{ t('common.save') }}</v-btn>
      </div>
    </v-card>

    <v-card class="pa-6 mb-4" max-width="860" :loading="loading">
      <div class="d-flex align-center mb-2">
        <div class="text-subtitle-1 font-weight-medium">{{ t('site.oidc') }}</div>
        <v-spacer />
        <v-btn size="small" color="primary" prepend-icon="mdi-plus" @click="openProvider">{{ t('site.addProvider') }}</v-btn>
      </div>
      <v-list v-if="providers.length" density="compact">
        <v-list-item v-for="p in providers" :key="p.id ?? p.name">
          <v-list-item-title class="font-weight-medium">
            {{ p.name }}
            <v-chip v-if="p.enabled === false" size="x-small" variant="tonal" class="ms-2">{{ t('site.providerDisabled') }}</v-chip>
          </v-list-item-title>
          <v-list-item-subtitle>{{ p.issuer }}</v-list-item-subtitle>
          <template #append>
            <v-btn icon="mdi-pencil" size="small" variant="text" :aria-label="t('common.edit')" @click="editProvider(p)" />
            <v-btn icon="mdi-delete" size="small" variant="text" color="error" :aria-label="t('common.delete')" @click="removeProvider(p)" />
          </template>
        </v-list-item>
      </v-list>
      <div v-else class="text-medium-emphasis py-2">{{ t('site.providersEmpty') }}</div>
    </v-card>

    <div class="d-flex justify-end mb-6" style="max-width: 860px">
      <v-btn color="primary" :loading="saving" :disabled="loading" @click="save">{{ t('common.save') }}</v-btn>
    </div>

    <v-dialog v-model="isSetupOpen" max-width="560">
      <v-card class="pa-6">
        <div class="text-h6 mb-4">{{ editingId ? t('site.editProvider') : t('site.addProviderTitle') }}</div>
        <v-form ref="pForm" @submit.prevent="saveProvider">
          <v-text-field v-model="pState.name" :label="t('site.providerName') + ' *'" :hint="t('site.providerNameHint')" persistent-hint :rules="[v => !!v || t('common.required')]" class="mb-2" />
          <div v-if="callbackBase" class="text-caption text-medium-emphasis mb-4">
            {{ t('site.callback') }}
            <code class="text-none">{{ callbackURL }}</code>
            <v-btn size="x-small" variant="text" prepend-icon="mdi-content-copy" class="ms-1 text-none" @click="copyCallback">{{ t('common.copy') }}</v-btn>
          </div>
          <v-text-field v-model="pState.issuer" :label="t('site.issuer') + ' *'" placeholder="https://accounts.google.com" :hint="t('site.issuerHint', { issuer: pState.issuer || 'https://idp.example.com' })" persistent-hint :rules="[v => !!v || t('common.required')]" class="mb-4" />
          <v-text-field v-model="pState.client_id" :label="t('site.clientId') + ' *'" :rules="[v => !!v || t('common.required')]" class="mb-4" />
          <v-text-field
            v-model="pState.client_secret" :label="t('site.clientSecret')" type="password"
            :rules="editingId ? [] : [v => !!v || t('common.required')]"
            :hint="editingId ? t('site.clientSecretHintEdit') : t('site.clientSecretHintNew')" persistent-hint
            class="mb-4"
          />
          <v-text-field
            v-model="pState.icon_url" :label="t('site.iconUrl')"
            :hint="t('site.iconUrlHint')"
            persistent-hint
            class="mb-4"
          />
          <v-text-field
            v-model="pState.scopes" :label="t('site.scopes')"
            :hint="t('site.scopesHint')"
            persistent-hint
            class="mb-4"
          />
          <v-textarea
            v-model="pState.claim_rules" :label="t('site.claimRules')" rows="3" placeholder='{"email_domains":["example.com"]}'
            :hint="t('site.claimRulesHint')"
            persistent-hint
            class="mb-2"
          />
          <v-switch v-model="pState.enabled" :label="t('site.providerEnabled')" color="primary" density="compact" hide-details class="mb-2" />
          <v-alert v-if="providerError" type="error" density="compact" class="mb-2">{{ providerError }}</v-alert>
          <div class="d-flex justify-end ga-2 mt-2">
            <v-btn variant="text" @click="isSetupOpen = false">{{ t('common.cancel') }}</v-btn>
            <v-btn color="primary" type="submit" :loading="savingProvider">{{ t('common.save') }}</v-btn>
          </div>
        </v-form>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar" :color="snackbarColor" location="top right" :timeout="4000">
      {{ snackbarText }}
    </v-snackbar>
  </div>
</template>
