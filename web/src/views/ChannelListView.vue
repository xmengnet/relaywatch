<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { listChannels, getNotifierTypes, createChannel, updateChannel, testChannel, type Channel, type NotifierMeta } from '@/api/channels'

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

async function reload() {
  loading.value = true
  try {
    const [c, t] = await Promise.all([listChannels(), getNotifierTypes()])
    channels.value = c.items
    types.value = t.items
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
function onTypeChange(t: string) {
  const next = types.value.find(x => x.type === t)
  const keep: Record<string, unknown> = {}
  for (const f of next?.fields ?? []) {
    if (f.key in state.config) keep[f.key] = state.config[f.key]
  }
  state.config = keep
}

async function submit() {
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
    snackbarText.value = r.ok ? `测试消息已发送到「${c.name}」` : `发送失败：${r.error}`
    snackbarColor.value = r.ok ? 'success' : 'error'
    snackbar.value = true
  } finally {
    testing.value = null
  }
}

async function doDelete(c: Channel) {
  if (!window.confirm(`删除渠道「${c.name}」？`)) return
  try {
    await import('@/api/channels').then(m => m.deleteChannel(c.id))
    await reload()
  } catch (e) {
    snackbarText.value = e instanceof Error ? e.message : String(e)
    snackbarColor.value = 'error'
    snackbar.value = true
  }
}

function typeLabel(t: string) {
  return types.value.find(x => x.type === t)?.label ?? t
}

function typeIcon(t: string) {
  return types.value.find(x => x.type === t)?.icon ?? 'mdi-bell-outline'
}

// 自定义正文模板示例（Go text/template，可用字段见 notifier.TemplateData）
const templateExample = [
  '📦 {{ .Monitor.Name }} 有新版本',
  '{{ if .Change.OldVersion }}{{ .Change.OldVersion }} → {{ end }}{{ .Change.NewVersion }}',
  '详情见：{{ .URL }}',
].join('\n')
</script>

<template>
  <div>
    <div class="d-flex align-center mb-4">
      <div class="text-h5">通知渠道</div>
      <v-spacer />
      <v-btn color="primary" prepend-icon="mdi-plus" @click="openNew">新建渠道</v-btn>
    </div>

    <v-progress-linear v-if="loading" indeterminate />

    <v-empty-state
      v-else-if="!channels.length"
      icon="mdi-bell-ring-outline"
      headline="还没有通知渠道"
      text="配置一个渠道，监控更新时自动推送"
    >
      <template #actions>
        <v-btn color="primary" prepend-icon="mdi-plus" @click="openNew">新建渠道</v-btn>
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
              {{ c.enabled ? '启用' : '停用' }}
            </v-chip>
          </div>
          <div class="text-caption text-medium-emphasis mb-4">创建于 {{ new Date(c.created_at).toLocaleString() }}</div>
          <div class="d-flex ga-2">
            <v-btn size="small" :loading="testing === c.id" prepend-icon="mdi-send" @click="doTest(c)">发送测试</v-btn>
            <v-spacer />
            <v-btn size="small" variant="text" prepend-icon="mdi-pencil" @click="openEdit(c)">编辑</v-btn>
            <v-btn size="small" variant="text" color="error" prepend-icon="mdi-delete" @click="doDelete(c)">删除</v-btn>
          </div>
        </v-card>
      </v-col>
    </v-row>

    <v-dialog v-model="dialog" max-width="640">
      <v-card class="pa-6">
        <div class="text-h6 mb-4">{{ editing ? '编辑渠道' : '新建渠道' }}</div>
        <v-form ref="form" @submit.prevent="submit">
          <v-text-field v-model="state.name" label="名称 *" :rules="[v => !!v || '名称不能为空']" />
          <v-select
            :model-value="state.type"
            :items="types"
            item-title="label"
            item-value="type"
            label="渠道类型"
            :disabled="!!editing"
            @update:model-value="onTypeChange($event); state.type = $event"
          />
          <template v-for="f in currentType?.fields ?? []" :key="f.key">
            <v-switch
              v-if="f.type === 'bool'"
              v-model="state.config[f.key]"
              :label="f.label"
              color="primary"
              :hint="f.help"
              persistent-hint
              class="mb-4"
            />
            <v-select
              v-else-if="f.type === 'select'"
              v-model="state.config[f.key] as string"
              :items="f.options ?? []"
              :label="f.label + (f.required ? ' *' : '')"
              :hint="f.help"
              persistent-hint
              class="mb-4"
            />
            <v-text-field
              v-else
              v-model="state.config[f.key] as string"
              :label="f.label + (f.required ? ' *' : '')"
              :hint="f.help"
              :placeholder="f.placeholder"
              :type="f.type === 'secret' ? 'password' : 'text'"
              persistent-hint
              class="mb-4"
            />
          </template>
          <v-textarea
            v-model="state.template"
            label="自定义模板（可选）"
            rows="3"
            hint="Go text/template，只作用于正文；留空用默认模板"
            persistent-hint
            class="mb-2"
          />
          <v-expand-transition>
            <div v-if="showTemplateHelp" class="text-caption mb-4">
              <div class="mb-1 text-medium-emphasis">示例（渲染效果：一行名称版本，一行链接）：</div>
              <pre class="text-pre-wrap bg-surface-lighten-2 pa-2 rounded text-mono">{{ templateExample }}</pre>
              <div class="mt-2 text-medium-emphasis">
                可用字段：<code>.Monitor.Name</code> / <code>.Monitor.SourceType</code> / <code>.Monitor.Tags</code>、
                <code>.Change.OldVersion</code> / <code>.Change.NewVersion</code> / <code>.Change.DetectedAt</code>、
                <code>.URL</code>；函数 <code>upper</code> 可用
              </div>
            </div>
          </v-expand-transition>
          <v-btn
            variant="text"
            size="small"
            :prepend-icon="showTemplateHelp ? 'mdi-chevron-up' : 'mdi-chevron-down'"
            class="mb-4 text-none"
            @click="showTemplateHelp = !showTemplateHelp"
          >模板写法与示例</v-btn>
          <v-switch v-if="editing" v-model="state.enabled" label="启用" color="primary" />
          <v-alert v-if="errorMsg" type="error" density="compact" class="mb-2">{{ errorMsg }}</v-alert>
          <div class="d-flex justify-end ga-2 mt-2">
            <v-btn variant="text" @click="dialog = false">取消</v-btn>
            <v-btn color="primary" type="submit" :loading="saving">保存</v-btn>
          </div>
        </v-form>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar" :color="snackbarColor" location="top right" :timeout="4000">
      {{ snackbarText }}
    </v-snackbar>
  </div>
</template>
