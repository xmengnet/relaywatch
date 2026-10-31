<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { listChannels, getNotifierTypes, createChannel, updateChannel, testChannel, type Channel, type NotifierMeta } from '@/api/channels'
import { useMetaText } from '@/composables/useMetaText'
import { useFormat } from '@/composables/useFormat'
import { useTemplatePresets, useTemplateVariables } from '@/constants/notifyTemplates'

const { t } = useI18n()
const { label: metaLabel, help: metaHelp, placeholder: metaPlaceholder } = useMetaText()
const { formatDateTime } = useFormat()
const auth = useAuthStore()
const loading = ref(true)
const channels = ref<Channel[]>([])
const types = ref<NotifierMeta[]>([])
const dialog = ref(false)
const saving = ref(false)
const testing = ref<number | null>(null)
const showTemplateHelp = ref(false)
const errorMsg = ref('')
const snackbar = ref(false)
const snackbarText = ref('')
const snackbarColor = ref('success')

const form = ref()
const editing = ref<Channel | null>(null)
const state = reactive({
  name: '',
  type: 'webhook',
  config: {} as Record<string, unknown>,
  template: '',
  enabled: true,
})

const currentType = computed(() => types.value.find(t => t.type === state.type))
const typeOptions = computed(() => types.value.map(x => ({ value: x.type, title: metaLabel(x) })))
const templateVariables = useTemplateVariables()
const templatePresets = useTemplatePresets()

async function reload() {
  loading.value = true
  try {
    const [c, tt] = await Promise.all([listChannels(), getNotifierTypes()])
    channels.value = c.items
    types.value = tt.items
  } finally {
    loading.value = false
  }
}

onMounted(reload)

function openNew() {
  editing.value = null
  state.name = ''
  state.type = types.value[0]?.type ?? 'webhook'
  state.config = {}
  state.template = ''
  state.enabled = true
  errorMsg.value = ''
  dialog.value = true
}

function openEdit(c: Channel) {
  editing.value = c
  state.name = c.name
  state.type = c.type
  try {
    state.config = JSON.parse(c.config || '{}')
  } catch {
    state.config = {}
  }
  state.template = c.template ?? ''
  state.enabled = c.enabled
  errorMsg.value = ''
  dialog.value = true
}

// 切换类型时保留已填的同名字段
function onTypeChange(nextType: string) {
  const next = types.value.find(x => x.type === nextType)
  const keep: Record<string, unknown> = {}
  for (const f of next?.fields ?? []) {
    if (f.key in state.config) keep[f.key] = state.config[f.key]
  }
  state.config = keep
}

async function submit() {
  if (saving.value) return // Enter 可绕过按钮 loading 拦截
  const { valid } = await form.value.validate()
  if (!valid) return
  saving.value = true
  errorMsg.value = ''
  const payload = {
    name: state.name,
    type: state.type,
    config: state.config,
    template: state.template || undefined,
  }
  try {
    if (editing.value) {
      await updateChannel(editing.value.id, { ...payload, enabled: state.enabled })
    } else {
      await createChannel(payload)
    }
    dialog.value = false
    await reload()
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e)
  } finally {
    saving.value = false
  }
}

async function doTest(c: Channel) {
  testing.value = c.id
  try {
    const r = await testChannel(c.id)
    snackbarText.value = r.ok
      ? t('channels.testSent', { name: c.name })
      : t('channels.testFailed', { error: r.error })
    snackbarColor.value = r.ok ? 'success' : 'error'
    snackbar.value = true
  } catch (e) {
    snackbarText.value = e instanceof Error ? e.message : String(e)
    snackbarColor.value = 'error'
    snackbar.value = true
  } finally {
    testing.value = null
  }
}

async function doDelete(c: Channel) {
  if (!window.confirm(t('channels.deleteConfirm', { name: c.name }))) return
  try {
    await import('@/api/channels').then(m => m.deleteChannel(c.id))
    await reload()
  } catch (e) {
    snackbarText.value = e instanceof Error ? e.message : String(e)
    snackbarColor.value = 'error'
    snackbar.value = true
  }
}

function typeLabel(type: string) {
  const meta = types.value.find(x => x.type === type)
  return meta ? metaLabel(meta) : type
}

function typeIcon(type: string) {
  return types.value.find(x => x.type === type)?.icon ?? 'mdi-bell-outline'
}

function applyPreset(tmpl: string) {
  state.template = tmpl
  snackbarText.value = t('channels.presetApplied')
  snackbarColor.value = 'success'
  snackbar.value = true
}

function insertVar(varCode: string) {
  if (!state.template) {
    state.template = varCode
  } else {
    state.template += (state.template.endsWith(' ') || state.template.endsWith('\n') ? '' : ' ') + varCode
  }
}
</script>

<template>
  <div>
    <div class="d-flex align-center mb-4">
      <div class="text-h5">{{ t('channels.title') }}</div>
      <v-spacer />
      <v-btn v-if="auth.can('channel.create')" color="primary" prepend-icon="mdi-plus" @click="openNew">{{ t('channels.new') }}</v-btn>
    </div>

    <v-progress-linear v-if="loading" indeterminate />

    <v-empty-state
      v-else-if="!channels.length"
      icon="mdi-bell-ring-outline"
      :headline="t('channels.empty')"
      :text="t('channels.emptyHint')"
    >
      <template #actions>
        <v-btn v-if="auth.can('channel.create')" color="primary" prepend-icon="mdi-plus" @click="openNew">{{ t('channels.new') }}</v-btn>
      </template>
    </v-empty-state>

    <v-row v-else>
      <v-col v-for="c in channels" :key="c.id" cols="12" md="6">
        <v-card class="pa-4">
          <div class="d-flex align-center mb-2">
            <v-icon class="me-2">{{ typeIcon(c.type) }}</v-icon>
            <div class="font-weight-medium">{{ c.name }}</div>
            <v-chip size="x-small" label variant="tonal" class="ms-2">{{ typeLabel(c.type) }}</v-chip>
            <v-spacer />
            <v-chip :color="c.enabled ? 'success' : 'default'" size="small" variant="tonal">
              {{ c.enabled ? t('channels.enabled') : t('channels.disabled') }}
            </v-chip>
          </div>
          <div class="text-caption text-medium-emphasis mb-4">{{ t('channels.createdAt', { time: formatDateTime(c.created_at) }) }}</div>
          <div class="d-flex ga-2">
            <!-- 写操作仅 admin/user 可见；viewer 只读（服务端同样拒绝） -->
            <v-btn v-if="auth.canWrite()" size="small" :loading="testing === c.id" prepend-icon="mdi-send" @click="doTest(c)">{{ t('channels.test') }}</v-btn>
            <v-spacer />
            <v-btn v-if="auth.canWrite()" size="small" variant="text" prepend-icon="mdi-pencil" @click="openEdit(c)">{{ t('common.edit') }}</v-btn>
            <v-btn v-if="auth.canWrite()" size="small" variant="text" color="error" prepend-icon="mdi-delete" @click="doDelete(c)">{{ t('common.delete') }}</v-btn>
          </div>
        </v-card>
      </v-col>
    </v-row>

    <v-dialog v-model="dialog" max-width="640">
      <v-card class="pa-6">
        <div class="text-h6 mb-4">{{ editing ? t('channels.dialogEdit') : t('channels.dialogNew') }}</div>
        <v-form ref="form" @submit.prevent="submit">
          <v-text-field v-model="state.name" :label="t('channels.name') + ' *'" :rules="[v => !!v || t('channels.nameRequired')]" />
          <v-select
            :model-value="state.type"
            :items="typeOptions"
            item-title="title"
            item-value="value"
            :label="t('channels.type')"
            :disabled="!!editing"
            @update:model-value="onTypeChange($event); state.type = $event"
          />
          <template v-for="f in currentType?.fields ?? []" :key="f.key">
            <v-switch
              v-if="f.type === 'bool'"
              v-model="state.config[f.key]"
              :label="metaLabel(f)"
              color="primary"
              :hint="metaHelp(f)"
              persistent-hint
              class="mb-4"
            />
            <v-select
              v-else-if="f.type === 'select'"
              v-model="state.config[f.key] as string"
              :items="f.options ?? []"
              :label="metaLabel(f) + (f.required ? ' *' : '')"
              :hint="metaHelp(f)"
              persistent-hint
              class="mb-4"
            />
            <v-text-field
              v-else
              v-model="state.config[f.key] as string"
              :label="metaLabel(f) + (f.required ? ' *' : '')"
              :hint="metaHelp(f)"
              :placeholder="metaPlaceholder(f)"
              :type="f.type === 'secret' ? 'password' : 'text'"
              persistent-hint
              class="mb-4"
            />
          </template>
          <v-textarea
            v-model="state.template"
            :label="t('channels.template')"
            rows="3"
            :hint="t('channels.templateHint')"
            persistent-hint
            class="mb-2"
          />

          <div class="d-flex align-center mb-2">
            <v-btn
              variant="tonal"
              size="small"
              color="primary"
              :prepend-icon="showTemplateHelp ? 'mdi-chevron-up' : 'mdi-code-braces'"
              class="text-none"
              @click="showTemplateHelp = !showTemplateHelp"
            >
              {{ showTemplateHelp ? t('channels.templateHide') : t('channels.templateShow') }}
            </v-btn>
            <v-spacer />
            <v-btn
              variant="text"
              size="small"
              color="secondary"
              to="/help?tab=channels"
              target="_blank"
              prepend-icon="mdi-open-in-new"
              class="text-caption text-none"
            >
              {{ t('common.viewDocs') }}
            </v-btn>
          </div>

          <v-expand-transition>
            <v-card v-if="showTemplateHelp" variant="outlined" class="pa-4 mb-4 bg-surface-lighten-1 rounded-lg">
              <!-- 快捷插入变量 -->
              <div class="text-subtitle-2 font-weight-bold mb-2 d-flex align-center">
                <v-icon size="16" class="me-1">mdi-variable</v-icon>
                {{ t('channels.templateInsert') }}
              </div>
              <div class="mb-3">
                <div v-for="g in templateVariables" :key="g.group" class="mb-2">
                  <span class="text-caption text-medium-emphasis me-2">{{ g.group }}:</span>
                  <div class="d-inline-flex flex-wrap ga-1">
                    <v-chip
                      v-for="f in g.fields"
                      :key="f.key"
                      size="x-small"
                      variant="outlined"
                      color="primary"
                      class="cursor-pointer"
                      @click="insertVar(f.key)"
                    >
                      <v-tooltip activator="parent" location="top">{{ f.desc }}</v-tooltip>
                      {{ f.key }}
                    </v-chip>
                  </div>
                </div>
              </div>

              <v-divider class="my-3" />

              <!-- 推荐预设模板 -->
              <div class="text-subtitle-2 font-weight-bold mb-2 d-flex align-center">
                <v-icon size="16" class="me-1">mdi-view-headline</v-icon>
                {{ t('channels.templatePresets') }}
              </div>
              <v-row dense>
                <v-col v-for="preset in templatePresets" :key="preset.key" cols="12">
                  <v-card variant="flat" class="pa-3 bg-surface border rounded">
                    <div class="d-flex align-center justify-space-between mb-1">
                      <span class="text-body-2 font-weight-bold">{{ preset.name }}</span>
                      <v-btn
                        size="x-small"
                        color="primary"
                        variant="tonal"
                        prepend-icon="mdi-content-copy"
                        @click="applyPreset(preset.template)"
                      >
                        {{ t('channels.applyPreset') }}
                      </v-btn>
                    </div>
                    <div class="text-caption text-medium-emphasis mb-2">{{ preset.desc }}</div>
                    <pre class="pa-2 bg-surface-lighten-2 rounded text-caption font-mono text-pre-wrap">{{ preset.template }}</pre>
                  </v-card>
                </v-col>
              </v-row>
            </v-card>
          </v-expand-transition>
          <v-switch v-if="editing" v-model="state.enabled" :label="t('channels.enabledLabel')" color="primary" />
          <v-alert v-if="errorMsg" type="error" density="compact" class="mb-2">{{ errorMsg }}</v-alert>
          <div class="d-flex justify-end ga-2 mt-2">
            <v-btn variant="text" @click="dialog = false">{{ t('common.cancel') }}</v-btn>
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

<style scoped>
.font-mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}
.cursor-pointer {
  cursor: pointer;
}
</style>
