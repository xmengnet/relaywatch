<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter, onBeforeRouteLeave } from 'vue-router'
import SourceConfigForm from '@/components/SourceConfigForm.vue'
import { createMonitor, getMonitor, updateMonitor, type MonitorPayload } from '@/api/monitors'
import { listChannels, type Channel } from '@/api/channels'

const route = useRoute()
const router = useRouter()

const isEdit = computed(() => !!route.params.id)
const form = ref()
const saving = ref(false)
const errorMsg = ref('')
const initial = ref<string>('')

const state = reactive({
  name: '',
  source_type: 'github_release',
  source_config: {} as Record<string, unknown>,
  version_prefix: '',
  version_suffix: '',
  version_include_regex: '',
  version_ignore_regex: '',
  interval_sec: 300,
  enabled: true,
  notify_cooldown_sec: 0,
  tags: [] as string[],
  note: '',
})

const allChannels = ref<Channel[]>([])
const selectedChannels = ref<number[]>([])

const intervalOptions = [
  { title: '1 分钟', value: 60 },
  { title: '5 分钟', value: 300 },
  { title: '15 分钟', value: 900 },
  { title: '30 分钟', value: 1800 },
  { title: '1 小时', value: 3600 },
  { title: '6 小时', value: 21600 },
  { title: '1 天', value: 86400 },
]

const tagsText = ref('')
function parseTags(): string[] {
  return tagsText.value.split(/[,，\s]+/).map(s => s.trim()).filter(Boolean)
}

onMounted(async () => {
  listChannels().then(r => { allChannels.value = r.items })
  if (isEdit.value) {
    const m = await getMonitor(route.params.id as string)
    Object.assign(state, {
      name: m.name,
      source_type: m.source_type,
      source_config: m.source_config ?? {},
      version_prefix: m.version_prefix ?? '',
      version_suffix: m.version_suffix ?? '',
      version_include_regex: m.version_include_regex ?? '',
      version_ignore_regex: m.version_ignore_regex ?? '',
      interval_sec: m.interval_sec,
      enabled: m.enabled,
      notify_cooldown_sec: m.notify_cooldown_sec,
      tags: m.tags ?? [],
      note: m.note ?? '',
    })
    tagsText.value = (m.tags ?? []).join(', ')
    selectedChannels.value = m.channel_ids ?? []
  }
  initial.value = JSON.stringify(snapshot())
})

function snapshot() {
  return { ...state, tags: parseTags(), channels: [...selectedChannels.value] }
}

const dirty = computed(() => JSON.stringify(snapshot()) !== initial.value)

onBeforeRouteLeave(async (_to, _from, next) => {
  if (!dirty.value || saving.value) return next()
  // 简单确认：浏览器原生对话框足够（自用工具定位）
  const ok = window.confirm('有未保存的修改，确定离开吗？')
  next(ok)
})

async function submit() {
  const { valid } = await form.value.validate()
  if (!valid) return
  saving.value = true
  errorMsg.value = ''
  const payload: MonitorPayload = {
    name: state.name,
    source_type: state.source_type,
    source_config: state.source_config,
    version_prefix: state.version_prefix || undefined,
    version_suffix: state.version_suffix || undefined,
    version_include_regex: state.version_include_regex || undefined,
    version_ignore_regex: state.version_ignore_regex || undefined,
    interval_sec: Number(state.interval_sec) || 300,
    enabled: state.enabled,
    notify_cooldown_sec: Number(state.notify_cooldown_sec) || 0,
    tags: parseTags(),
    note: state.note || undefined,
    channel_ids: selectedChannels.value,
  }
  try {
    if (isEdit.value) {
      await updateMonitor(route.params.id as string, payload)
    } else {
      await createMonitor(payload)
    }
    router.push('/monitors')
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="d-flex align-center mb-4">
    <v-btn icon="mdi-arrow-left" variant="text" to="/monitors" aria-label="返回" />
    <div class="text-h5">{{ isEdit ? '编辑监控' : '新建监控' }}</div>
  </div>

  <v-form ref="form" @submit.prevent="submit">
    <v-row>
      <v-col cols="12" md="8">
        <v-card class="pa-6 mb-4">
          <div class="text-subtitle-1 font-weight-medium mb-4">基本信息</div>
          <v-text-field v-model="state.name" label="名称 *" :rules="[v => !!v || '名称不能为空']" required />
          <v-text-field v-model="tagsText" label="标签" hint="逗号或空格分隔" persistent-hint />
          <v-textarea v-model="state.note" label="备注" rows="2" />
        </v-card>

        <v-card class="pa-6 mb-4">
          <div class="text-subtitle-1 font-weight-medium mb-4">监控源</div>
          <SourceConfigForm
            v-model:source-type="state.source_type"
            v-model="state.source_config"
          />
        </v-card>

        <v-card class="pa-6 mb-4">
          <div class="text-subtitle-1 font-weight-medium mb-4">版本处理</div>
          <v-row>
            <v-col cols="6">
              <v-text-field v-model="state.version_prefix" label="前缀剥离" hint="如 v" persistent-hint />
            </v-col>
            <v-col cols="6">
              <v-text-field v-model="state.version_suffix" label="后缀剥离" hint="如 -1" persistent-hint />
            </v-col>
            <v-col cols="6">
              <v-text-field v-model="state.version_include_regex" label="只跟踪匹配" hint="正则，如 ^1\." persistent-hint />
            </v-col>
            <v-col cols="6">
              <v-text-field v-model="state.version_ignore_regex" label="忽略匹配" hint="正则，如 nightly" persistent-hint />
            </v-col>
          </v-row>
        </v-card>

        <v-card class="pa-6">
          <div class="text-subtitle-1 font-weight-medium mb-4">检查设置</div>
          <v-row>
            <v-col cols="12" md="6">
              <v-select v-model="state.interval_sec" :items="intervalOptions" item-title="title" item-value="value" label="检查间隔" />
            </v-col>
            <v-col cols="12" md="6">
              <v-text-field v-model.number="state.notify_cooldown_sec" label="通知静默期（秒）" type="number" min="0" hint="同一变更周期内的合并间隔" persistent-hint />
            </v-col>
          </v-row>
          <v-switch v-model="state.enabled" label="启用" color="primary" />
        </v-card>

        <v-card class="pa-6">
          <div class="text-subtitle-1 font-weight-medium mb-4">通知渠道</div>
          <div v-if="!allChannels.length" class="text-medium-emphasis mb-2">
            还没有渠道。<router-link to="/channels">去创建</router-link>
          </div>
          <v-checkbox
            v-for="c in allChannels"
            :key="c.id"
            v-model="selectedChannels"
            :value="c.id"
            :label="`${c.name}（${c.type}）`"
            color="primary"
            density="comfortable"
            hide-details
          />
        </v-card>
      </v-col>

      <v-col cols="12" md="4">
        <v-card class="pa-6 position-sticky" style="top: 80px">
          <v-alert v-if="errorMsg" type="error" density="compact" class="mb-4">{{ errorMsg }}</v-alert>
          <v-btn type="submit" block color="primary" :loading="saving">保存</v-btn>
          <v-btn block variant="text" class="mt-2" to="/monitors">取消</v-btn>
        </v-card>
      </v-col>
    </v-row>
  </v-form>
</template>
