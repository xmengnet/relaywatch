<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import StatusChip from '@/components/StatusChip.vue'
import RelativeTime from '@/components/RelativeTime.vue'
import VersionDiff from '@/components/VersionDiff.vue'
import { getMonitor, checkNow, ackMonitor, listHistory, listChanges, type CheckHistoryItem, type VersionChangeItem } from '@/api/monitors'
import type { Monitor } from '@/api/types'

const route = useRoute()
const router = useRouter()

const monitor = ref<Monitor | null>(null)
const checking = ref(false)
const acking = ref(false)
const checkResult = ref<{ ok: boolean; version?: string; error?: string; state: string } | null>(null)
const history = ref<CheckHistoryItem[]>([])
const changes = ref<VersionChangeItem[]>([])
const snackbar = ref(false)
const snackbarText = ref('')
const snackbarColor = ref('success')

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

async function reload() {
  monitor.value = await getMonitor(id.value)
  const [h, c] = await Promise.all([listHistory(id.value, 1, 10), listChanges(id.value, 1, 10)])
  history.value = h.items
  changes.value = c.items
}

async function doCheck() {
  checking.value = true
  checkResult.value = null
  try {
    const r = await checkNow(id.value)
    checkResult.value = r
    snackbarText.value = r.ok ? `检查成功：${r.version ?? '—'}（${r.duration_ms}ms）` : `检查失败：${r.error}`
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
    snackbarText.value = '已确认更新'
    snackbarColor.value = 'success'
    snackbar.value = true
    await reload()
  } finally {
    acking.value = false
  }
}

async function doDelete() {
  if (!window.confirm(`删除监控「${monitor.value?.name}」？此操作不可恢复。`)) return
  await import('@/api/monitors').then(m => m.deleteMonitor(id.value))
  router.push('/monitors')
}
</script>

<template>
  <div v-if="monitor">
    <div class="d-flex align-center flex-wrap ga-2 mb-4">
      <v-btn icon="mdi-arrow-left" variant="text" to="/monitors" aria-label="返回" />
      <div class="text-h5">{{ monitor.name }}</div>
      <StatusChip :state="monitor.state" />
      <v-spacer />
      <v-btn color="primary" :loading="checking" prepend-icon="mdi-radar" @click="doCheck">立即检查</v-btn>
      <v-btn v-if="canAck" color="warning" :loading="acking" prepend-icon="mdi-check" @click="doAck">确认更新</v-btn>
      <v-btn variant="text" :to="`/monitors/${id}/edit`" prepend-icon="mdi-pencil">编辑</v-btn>
      <v-btn variant="text" color="error" prepend-icon="mdi-delete" @click="doDelete">删除</v-btn>
    </div>

    <v-row>
      <v-col cols="12" md="8">
        <v-card class="pa-6 mb-4">
          <div class="text-subtitle-1 font-weight-medium mb-4">配置</div>
          <v-row dense>
            <v-col cols="12" md="6"><b>源类型</b>：{{ monitor.source_type }}</v-col>
            <v-col cols="12" md="6"><b>间隔</b>：{{ monitor.interval_sec }} 秒</v-col>
            <v-col cols="12"><b>源参数</b>：<code class="text-caption">{{ sourceSummary }}</code></v-col>
            <v-col v-if="monitor.tags?.length" cols="12">
              <v-chip v-for="t in monitor.tags" :key="t" size="small" class="me-1" label>{{ t }}</v-chip>
            </v-col>
            <v-col v-if="monitor.note" cols="12"><b>备注</b>：{{ monitor.note }}</v-col>
          </v-row>
        </v-card>

        <v-card class="pa-6 mb-4">
          <div class="text-subtitle-1 font-weight-medium mb-4">版本历史</div>
          <v-table v-if="changes.length" density="comfortable">
            <thead>
              <tr><th>变更</th><th>发现时间</th><th>状态</th></tr>
            </thead>
            <tbody>
              <tr v-for="c in changes" :key="c.id">
                <td><VersionDiff :old-version="c.old_version" :new-version="c.new_version" /></td>
                <td><RelativeTime :time="c.detected_at" /></td>
                <td>
                  <v-chip v-if="c.acknowledged_at" size="small" variant="tonal">已确认</v-chip>
                  <v-chip v-else size="small" color="warning" variant="tonal">待确认</v-chip>
                </td>
              </tr>
            </tbody>
          </v-table>
          <div v-else class="text-medium-emphasis">暂无版本变更</div>
        </v-card>

        <v-card class="pa-6">
          <div class="text-subtitle-1 font-weight-medium mb-4">最近检查</div>
          <v-table v-if="history.length" density="comfortable">
            <thead>
              <tr><th>时间</th><th>结果</th><th>版本</th><th>耗时</th><th>错误</th></tr>
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
          <div v-else class="text-medium-emphasis">还没有检查记录，点「立即检查」试试</div>
        </v-card>
      </v-col>

      <v-col cols="12" md="4">
        <v-card class="pa-6 position-sticky" style="top: 80px">
          <div class="d-flex align-center mb-4">
            <StatusChip :state="monitor.state" />
          </div>
          <div class="d-flex justify-space-between py-1">
            <span class="text-medium-emphasis">当前版本</span>
            <code>{{ monitor.current_version || '—' }}</code>
          </div>
          <div class="d-flex justify-space-between py-1">
            <span class="text-medium-emphasis">最新版本</span>
            <VersionDiff :old-version="monitor.current_version" :new-version="monitor.latest_version" />
          </div>
          <div class="d-flex justify-space-between py-1">
            <span class="text-medium-emphasis">上次检查</span>
            <span><RelativeTime :time="monitor.last_checked_at" /></span>
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
