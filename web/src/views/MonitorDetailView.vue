<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import StatusChip from '@/components/StatusChip.vue'
import RelativeTime from '@/components/RelativeTime.vue'
import VersionDiff from '@/components/VersionDiff.vue'
import { useSse } from '@/composables/useSse'
import { useAuthStore } from '@/stores/auth'
import { getMonitor, checkNow, ackMonitor, listHistory, listChanges, type CheckHistoryItem, type VersionChangeItem } from '@/api/monitors'
import type { Monitor } from '@/api/types'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const monitor = ref<Monitor | null>(null)
const checking = ref(false)
const acking = ref(false)
const history = ref<CheckHistoryItem[]>([])
const changes = ref<VersionChangeItem[]>([])
const snackbar = ref(false)
const snackbarText = ref('')
const snackbarColor = ref('success')
const refMonitorName = ref('')

const id = computed(() => route.params.id as string)

const sourceSummary = computed(() => {
  if (!monitor.value) return ''
  try {
    const cfg = typeof monitor.value.source_config === 'string'
      ? JSON.parse(monitor.value.source_config)
      : monitor.value.source_config
    return Object.entries(cfg ?? {}).map(([k, v]) => `${k}=${String(v)}`).join('  ')
  } catch {
    return String(monitor.value.source_config)
  }
})

const canAck = computed(() => monitor.value?.state === 'has_update')

onMounted(async () => {
  await reload()
})

// 同路由组件复用（如「参照监控」链接从 /monitors/1 跳 /monitors/2）：
// onMounted 不会重跑，必须 watch 路由参数否则整页显示旧监控
watch(id, () => {
  reload()
})

async function reload() {
  monitor.value = await getMonitor(id.value)
  // 后端现已随监控返回 reference_monitor_name；旧字段兜底逻辑保留
  if (monitor.value?.reference_monitor_id && !monitor.value.reference_monitor_name) {
    getMonitor(monitor.value.reference_monitor_id)
      .then(r => { refMonitorName.value = r.name })
      .catch(() => { refMonitorName.value = '' })
  } else {
    refMonitorName.value = ''
  }
  const [h, c] = await Promise.all([listHistory(id.value, 1, 10), listChanges(id.value, 1, 10)])
  history.value = h.items
  changes.value = c.items
}

const refDisplayName = computed(() =>
  monitor.value?.reference_monitor_name || refMonitorName.value || `#${monitor.value?.reference_monitor_id}`)

// SSE 实时更新：收到本条监控的检查完成通知时立即刷新详情与历史
useSse('check.done', (data: any) => {
  if (data && String(data.monitor_id) === String(id.value)) {
    reload()
  }
})

async function doCheck() {
  checking.value = true
  try {
    const r = await checkNow(id.value)
    snackbarText.value = r.ok
      ? t('monitors.detail.checkSuccess', { version: r.version ?? '—', ms: r.duration_ms })
      : t('monitors.detail.checkFailed', { error: r.error })
    snackbarColor.value = r.ok ? 'success' : 'error'
    snackbar.value = true
    await reload()
  } catch (e) {
    snackbarText.value = e instanceof Error ? e.message : String(e)
    snackbarColor.value = 'error'
    snackbar.value = true
  } finally {
    checking.value = false
  }
}

async function doAck() {
  acking.value = true
  try {
    await ackMonitor(id.value)
    snackbarText.value = t('monitors.detail.ackSuccess')
    snackbarColor.value = 'success'
    snackbar.value = true
    await reload()
  } catch (e) {
    snackbarText.value = e instanceof Error ? e.message : String(e)
    snackbarColor.value = 'error'
    snackbar.value = true
  } finally {
    acking.value = false
  }
}

async function doDelete() {
  if (!window.confirm(t('monitors.detail.deleteConfirm', { name: monitor.value?.name }))) return
  try {
    await import('@/api/monitors').then(m => m.deleteMonitor(id.value))
    router.push('/monitors')
  } catch (e) {
    snackbarText.value = e instanceof Error ? e.message : String(e)
    snackbarColor.value = 'error'
    snackbar.value = true
  }
}

function isSafeUrl(url?: string): boolean {
  if (!url) return false
  return /^https?:\/\//i.test(url.trim())
}
</script>

<template>
  <div v-if="monitor">
    <div class="d-flex align-center flex-wrap ga-2 mb-4">
      <v-btn icon="mdi-arrow-left" variant="text" to="/monitors" :aria-label="t('common.back')" />
      <div class="text-h5">{{ monitor.name }}</div>
      <StatusChip :state="monitor.state" />
      <v-spacer />
      <!-- 写操作仅 admin/user 可见；viewer 只读（服务端同样拒绝） -->
      <v-btn v-if="auth.canWrite()" color="primary" :loading="checking" prepend-icon="mdi-radar" @click="doCheck">{{ t('monitors.checkNow') }}</v-btn>
      <v-btn v-if="canAck && auth.canWrite()" color="warning" :loading="acking" prepend-icon="mdi-check" @click="doAck">{{ t('monitors.detail.ack') }}</v-btn>
      <v-btn v-if="auth.canWrite()" variant="text" :to="`/monitors/${id}/edit`" prepend-icon="mdi-pencil">{{ t('common.edit') }}</v-btn>
      <v-btn v-if="auth.canWrite()" variant="text" color="error" prepend-icon="mdi-delete" @click="doDelete">{{ t('common.delete') }}</v-btn>
    </div>

    <v-row>
      <v-col cols="12" md="8">
        <v-card class="pa-6 mb-4">
          <div class="text-subtitle-1 font-weight-medium mb-4">{{ t('monitors.detail.config') }}</div>
          <v-row dense>
            <v-col cols="12" md="6"><b>{{ t('monitors.detail.sourceType') }}</b>：{{ monitor.source_type }}</v-col>
            <v-col cols="12" md="6"><b>{{ t('monitors.detail.interval') }}</b>：{{ t('monitors.detail.intervalValue', { n: monitor.interval_sec }) }}</v-col>
            <v-col cols="12"><b>{{ t('monitors.detail.sourceConfig') }}</b>：<code class="text-caption">{{ sourceSummary }}</code></v-col>
            <v-col v-if="monitor.tags?.length" cols="12">
              <v-chip v-for="tag in monitor.tags" :key="tag" size="small" class="me-1" label>{{ tag }}</v-chip>
            </v-col>
            <v-col v-if="monitor.note" cols="12"><b>{{ t('monitors.detail.note') }}</b>：{{ monitor.note }}</v-col>
          </v-row>
        </v-card>

        <v-card class="pa-6 mb-4">
          <div class="text-subtitle-1 font-weight-medium mb-4">{{ t('monitors.detail.versionHistory') }}</div>
          <v-table v-if="changes.length" density="comfortable">
            <thead>
              <tr>
                <th>{{ t('monitors.detail.change') }}</th>
                <th>{{ t('monitors.detail.detectedAt') }}</th>
                <th>{{ t('common.status') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="c in changes" :key="c.id">
                <td>
                  <div class="d-inline-flex align-center ga-1">
                    <VersionDiff :old-version="c.old_version" :new-version="c.new_version" />
                    <v-btn
                      v-if="isSafeUrl(c.url)"
                      size="x-small"
                      variant="text"
                      color="primary"
                      density="compact"
                      class="ms-1"
                      :href="c.url"
                      target="_blank"
                      rel="noopener noreferrer"
                      :aria-label="t('monitors.detail.openReleaseUrl')"
                    >
                      <v-icon size="small">mdi-open-in-new</v-icon>
                      <v-tooltip activator="parent" location="top">{{ t('monitors.detail.openReleaseUrl') }}</v-tooltip>
                    </v-btn>
                  </div>
                </td>
                <td><RelativeTime :time="c.detected_at" /></td>
                <td>
                  <v-chip v-if="c.acknowledged_at" size="small" variant="tonal">{{ t('monitors.detail.acknowledged') }}</v-chip>
                  <v-chip v-else size="small" color="warning" variant="tonal">{{ t('monitors.detail.pendingAck') }}</v-chip>
                </td>
              </tr>
            </tbody>
          </v-table>
          <div v-else class="text-medium-emphasis">{{ t('monitors.detail.noChanges') }}</div>
        </v-card>

        <v-card class="pa-6">
          <div class="text-subtitle-1 font-weight-medium mb-4">{{ t('monitors.detail.recentChecks') }}</div>
          <v-table v-if="history.length" density="comfortable">
            <thead>
              <tr>
                <th>{{ t('monitors.detail.time') }}</th>
                <th>{{ t('monitors.detail.result') }}</th>
                <th>{{ t('monitors.detail.version') }}</th>
                <th>{{ t('monitors.detail.duration') }}</th>
                <th>{{ t('monitors.detail.error') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="h in history" :key="h.id">
                <td><RelativeTime :time="h.checked_at" /></td>
                <td>
                  <v-icon v-if="h.ok" color="success" size="small">mdi-check-circle</v-icon>
                  <v-icon v-else color="error" size="small">mdi-alert-circle</v-icon>
                </td>
                <td><code>{{ h.version || '—' }}</code></td>
                <td>{{ h.duration_ms }}ms</td>
                <td class="text-error text-caption">{{ h.error || '' }}</td>
              </tr>
            </tbody>
          </v-table>
          <div v-else class="text-medium-emphasis">{{ t('monitors.detail.noChecks') }}</div>
        </v-card>
      </v-col>

      <v-col cols="12" md="4">
        <v-card class="pa-6 position-sticky" style="top: 80px">
          <div class="d-flex align-center mb-4">
            <StatusChip :state="monitor.state" />
          </div>
          <div class="d-flex justify-space-between py-1">
            <span class="text-medium-emphasis">{{ t('monitors.detail.currentVersion') }}</span>
            <code>{{ monitor.current_version || '—' }}</code>
          </div>
          <div class="d-flex justify-space-between py-1">
            <span class="text-medium-emphasis">{{ t('monitors.detail.latestVersion') }}</span>
            <VersionDiff :old-version="monitor.current_version" :new-version="monitor.latest_version" />
          </div>
          <div class="d-flex justify-space-between py-1">
            <span class="text-medium-emphasis">{{ t('monitors.detail.lastChecked') }}</span>
            <span><RelativeTime :time="monitor.last_checked_at" /></span>
          </div>
          <div v-if="monitor.reference_monitor_id" class="mt-4 pt-4 border-t">
            <div class="text-subtitle-2 font-weight-medium mb-2">{{ t('monitors.detail.compareTitle') }}</div>
            <div class="d-flex justify-space-between align-center py-1">
              <span class="text-medium-emphasis">{{ t('monitors.detail.compareState') }}</span>
              <v-chip v-if="monitor.compare_state === 'lagging'" size="small" color="error" variant="flat" label>{{ t('compare.lagging') }}</v-chip>
              <v-chip v-else-if="monitor.compare_state === 'in_sync'" size="small" color="success" variant="tonal" label>{{ t('compare.in_sync') }}</v-chip>
              <v-chip v-else-if="monitor.compare_state === 'ahead'" size="small" color="purple" variant="tonal" label>{{ t('compare.ahead') }}</v-chip>
              <v-chip v-else size="small" variant="text" label>{{ t('compare.pending') }}</v-chip>
            </div>
            <div class="d-flex justify-space-between py-1">
              <span class="text-medium-emphasis">{{ t('monitors.detail.referenceMonitor') }}</span>
              <router-link :to="`/monitors/${monitor.reference_monitor_id}`" class="text-decoration-none">
                {{ refDisplayName }}
              </router-link>
            </div>
            <div class="d-flex justify-space-between py-1">
              <span class="text-medium-emphasis">{{ t('monitors.detail.localVersion') }}</span>
              <code>{{ monitor.current_version || '—' }}</code>
            </div>
            <div class="d-flex justify-space-between py-1">
              <span class="text-medium-emphasis">{{ t('monitors.detail.refVersion') }}</span>
              <code>{{ monitor.compare_ref_version || '—' }}</code>
            </div>
            <div v-if="monitor.compare_since" class="d-flex justify-space-between py-1">
              <span class="text-medium-emphasis">{{ t('monitors.detail.statusSince') }}</span>
              <span><RelativeTime :time="monitor.compare_since" /></span>
            </div>
          </div>
          <div v-if="monitor.last_error" class="mt-2">
            <v-alert type="error" density="compact" variant="tonal">{{ monitor.last_error }}</v-alert>
          </div>
        </v-card>
      </v-col>
    </v-row>

    <v-snackbar v-model="snackbar" :color="snackbarColor" location="top right" :timeout="4000">
      {{ snackbarText }}
    </v-snackbar>
  </div>

  <div v-else class="text-center py-12">
    <v-progress-circular indeterminate />
  </div>
</template>
