<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import SourceConfigForm from '@/components/SourceConfigForm.vue'
import { createMonitor, getMonitor, listMonitors, updateMonitor, type MonitorPayload } from '@/api/monitors'
import type { Monitor } from '@/api/types'
import { listChannels, type Channel } from '@/api/channels'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const isEdit = computed(() => !!route.params.id)
const form = ref()
const saving = ref(false)
const errorMsg = ref('')

const state = reactive({
  name: '',
  source_type: 'github_release',
  source_config: {} as Record<string, unknown>,
  version_prefix: '',
  version_suffix: '',
  version_include_regex: '',
  version_ignore_regex: '',
  from_pattern: '',
  to_pattern: '',
  interval_sec: 300,
  enabled: true,
  notify_cooldown_sec: 0,
  tags: [] as string[],
  note: '',
  reference_monitor_id: null as number | null,
})

const allChannels = ref<Channel[]>([])
const selectedChannels = ref<number[]>([])
const referenceOptions = ref<Array<{ title: string; value: number | null }>>([])

const intervalOptions = computed(() => [
  { title: t('monitors.intervals.m1'), value: 60 },
  { title: t('monitors.intervals.m5'), value: 300 },
  { title: t('monitors.intervals.m15'), value: 900 },
  { title: t('monitors.intervals.m30'), value: 1800 },
  { title: t('monitors.intervals.h1'), value: 3600 },
  { title: t('monitors.intervals.h6'), value: 21600 },
  { title: t('monitors.intervals.d1'), value: 86400 },
])

const tagsText = ref('')
function parseTags(): string[] {
  return tagsText.value.split(/[,，\s]+/).map(s => s.trim()).filter(Boolean)
}

// 参考监控候选：后端钳制 per_page≤100，固定大值会被静默截断，逐页拉全
async function loadReferenceOptions() {
  const currentId = route.params.id ? Number(route.params.id) : null
  const valid: Monitor[] = []
  try {
    let fetched = 0
    for (let page = 1; ; page++) {
      const r = await listMonitors({ page, per_page: 100 })
      fetched += r.items.length
      valid.push(...r.items.filter(item => item.id !== currentId && !item.reference_monitor_id))
      if (r.items.length === 0 || fetched >= r.total) break
    }
  } catch {
    /* 拉取失败时下拉为空，不阻塞表单 */
  }
  referenceOptions.value = [
    { title: t('monitors.edit.noReference'), value: null },
    ...valid.map(item => ({
      title: `${item.name} (${item.source_type})`,
      value: item.id,
    })),
  ]
}

onMounted(async () => {
  listChannels().then(r => { allChannels.value = r.items })
  loadReferenceOptions()
  if (isEdit.value) {
    const m = await getMonitor(route.params.id as string)
    // 后端可能返回 JSON 字符串（旧版本）或对象，统一归一化为对象
    let cfg: Record<string, unknown> = {}
    if (typeof m.source_config === 'string') {
      try { cfg = JSON.parse(m.source_config || '{}') } catch { cfg = {} }
    } else if (m.source_config) {
      cfg = m.source_config
    }
    Object.assign(state, {
      name: m.name,
      source_type: m.source_type,
      source_config: cfg,
      version_prefix: m.version_prefix ?? '',
      version_suffix: m.version_suffix ?? '',
      version_include_regex: m.version_include_regex ?? '',
      version_ignore_regex: m.version_ignore_regex ?? '',
      from_pattern: m.from_pattern ?? '',
      to_pattern: m.to_pattern ?? '',
      interval_sec: m.interval_sec,
      enabled: m.enabled,
      notify_cooldown_sec: m.notify_cooldown_sec,
      tags: m.tags ?? [],
      note: m.note ?? '',
      reference_monitor_id: m.reference_monitor_id ?? null,
    })
    tagsText.value = (m.tags ?? []).join(', ')
    selectedChannels.value = m.channel_ids ?? []
  }
})

async function submit() {
  if (saving.value) return // Enter 键可绕过按钮 loading 的 pointer-events 拦截
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
    from_pattern: state.from_pattern || undefined,
    to_pattern: state.to_pattern || undefined,
    interval_sec: Number(state.interval_sec) || 300,
    enabled: state.enabled,
    notify_cooldown_sec: Number(state.notify_cooldown_sec) || 0,
    tags: parseTags(),
    note: state.note || undefined,
    channel_ids: selectedChannels.value,
    reference_monitor_id: state.reference_monitor_id,
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
    <v-btn icon="mdi-arrow-left" variant="text" to="/monitors" :aria-label="t('common.back')" />
    <div class="text-h5">{{ isEdit ? t('monitors.editTitle') : t('monitors.newTitle') }}</div>
  </div>

  <v-form ref="form" @submit.prevent="submit">
    <v-row>
      <v-col cols="12" md="8">
        <v-card class="pa-6 mb-4">
          <div class="text-subtitle-1 font-weight-medium mb-4">{{ t('monitors.edit.basic') }}</div>
          <v-text-field v-model="state.name" :label="t('monitors.edit.name') + ' *'" :rules="[v => !!v || t('monitors.edit.nameRequired')]" required />
          <v-text-field v-model="tagsText" :label="t('monitors.edit.tags')" :hint="t('monitors.edit.tagsHint')" persistent-hint />
          <v-textarea v-model="state.note" :label="t('monitors.edit.note')" rows="2" />
        </v-card>

        <v-card class="pa-6 mb-4">
          <div class="text-subtitle-1 font-weight-medium mb-4">{{ t('monitors.edit.source') }}</div>
          <SourceConfigForm
            v-model:source-type="state.source_type"
            v-model="state.source_config"
          />
        </v-card>

        <v-card class="pa-6 mb-4">
          <div class="text-subtitle-1 font-weight-medium mb-4">{{ t('monitors.edit.versionProcessing') }}</div>
          <v-row>
            <v-col cols="6">
              <v-text-field v-model="state.version_prefix" :label="t('monitors.edit.prefix')" :hint="t('monitors.edit.prefixHint')" persistent-hint />
            </v-col>
            <v-col cols="6">
              <v-text-field v-model="state.version_suffix" :label="t('monitors.edit.suffix')" :hint="t('monitors.edit.suffixHint')" persistent-hint />
            </v-col>
            <v-col cols="6">
              <v-text-field v-model="state.version_include_regex" :label="t('monitors.edit.include')" :hint="t('monitors.edit.includeHint')" persistent-hint />
            </v-col>
            <v-col cols="6">
              <v-text-field v-model="state.version_ignore_regex" :label="t('monitors.edit.ignore')" :hint="t('monitors.edit.ignoreHint')" persistent-hint />
            </v-col>
            <v-col cols="6">
              <v-text-field v-model="state.from_pattern" :label="t('monitors.edit.fromPattern')" :hint="t('monitors.edit.fromHint')" persistent-hint />
            </v-col>
            <v-col cols="6">
              <v-text-field v-model="state.to_pattern" :label="t('monitors.edit.toPattern')" :hint="t('monitors.edit.toHint')" persistent-hint />
            </v-col>
          </v-row>
        </v-card>

        <v-card class="pa-6 mb-4">
          <div class="text-subtitle-1 font-weight-medium mb-1">{{ t('monitors.edit.compare') }}</div>
          <div class="text-body-2 text-medium-emphasis mb-4">{{ t('monitors.edit.compareDesc') }}</div>
          <v-select
            v-model="state.reference_monitor_id"
            :items="referenceOptions"
            item-title="title"
            item-value="value"
            :label="t('monitors.edit.reference')"
            :hint="t('monitors.edit.referenceHint')"
            persistent-hint
          />
        </v-card>

        <v-card class="pa-6">
          <div class="text-subtitle-1 font-weight-medium mb-4">{{ t('monitors.edit.checkSettings') }}</div>
          <v-row>
            <v-col cols="12" md="6">
              <v-select v-model="state.interval_sec" :items="intervalOptions" item-title="title" item-value="value" :label="t('monitors.edit.interval')" />
            </v-col>
            <v-col cols="12" md="6">
              <v-text-field v-model.number="state.notify_cooldown_sec" :label="t('monitors.edit.cooldown')" type="number" min="0" :hint="t('monitors.edit.cooldownHint')" persistent-hint />
            </v-col>
          </v-row>
          <v-switch v-model="state.enabled" :label="t('monitors.edit.enabled')" color="primary" />
        </v-card>

        <v-card class="pa-6">
          <div class="text-subtitle-1 font-weight-medium mb-4">{{ t('monitors.edit.channels') }}</div>
          <div v-if="!allChannels.length" class="text-medium-emphasis mb-2">
            {{ t('monitors.edit.noChannels') }}<router-link to="/channels">{{ t('monitors.edit.goCreate') }}</router-link>
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
          <v-btn type="submit" block color="primary" :loading="saving">{{ t('common.save') }}</v-btn>
          <v-btn block variant="text" class="mt-2" to="/monitors">{{ t('common.cancel') }}</v-btn>
        </v-card>
      </v-col>
    </v-row>
  </v-form>
</template>
