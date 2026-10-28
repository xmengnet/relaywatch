<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getStats, listMonitors } from '@/api/monitors'
import { useSse } from '@/composables/useSse'
import StatusChip from '@/components/StatusChip.vue'
import RelativeTime from '@/components/RelativeTime.vue'
import VersionDiff from '@/components/VersionDiff.vue'
import type { Monitor } from '@/api/types'

const { t } = useI18n()

const stats = ref<{ total: number; has_update: number; error: number; ok: number; missing: number; unknown: number; lagging?: number } | null>(null)
const attention = ref<Monitor[]>([])

async function refreshDashboard() {
  stats.value = await getStats()
  // 需要关注：lagging + has_update + error，最多 5 条
  const [upd, err, lag] = await Promise.all([
    listMonitors({ state: 'has_update', per_page: 5 }),
    listMonitors({ state: 'error', per_page: 5 }),
    listMonitors({ compare: 'lagging', per_page: 5 }),
  ])
  const map = new Map<number, Monitor>()
  for (const item of [...lag.items, ...upd.items, ...err.items]) {
    map.set(item.id, item)
  }
  attention.value = Array.from(map.values()).slice(0, 5)
}

onMounted(() => {
  refreshDashboard()
})

// SSE 实时更新：检查完成或投递完成时无刷新更新仪表盘
useSse('check.done', () => { refreshDashboard() })
useSse('delivery.done', () => { refreshDashboard() })

const cards = computed(() => [
  { key: 'total', label: t('dashboard.total'), color: 'primary', icon: 'mdi-radar', to: '/monitors' },
  { key: 'has_update', label: t('dashboard.updates'), color: 'warning', icon: 'mdi-arrow-up-circle', to: '/monitors?state=has_update' },
  { key: 'lagging', label: t('dashboard.lagging'), color: 'deep-orange', icon: 'mdi-clock-alert-outline', to: '/monitors?compare=lagging' },
  { key: 'error', label: t('dashboard.errors'), color: 'error', icon: 'mdi-alert-circle', to: '/monitors?state=error' },
] as const)
</script>

<template>
  <div v-if="stats">
    <div class="text-h5 mb-6">{{ t('dashboard.title') }}</div>

    <v-row class="mb-6">
      <v-col v-for="c in cards" :key="c.key" cols="12" sm="6" md="3">
        <v-card :to="c.to" class="pa-4 d-flex align-center" hover>
          <v-icon :color="c.color" size="36" class="me-4">{{ c.icon }}</v-icon>
          <div>
            <div class="text-h4 font-weight-bold">{{ stats[c.key] }}</div>
            <div class="text-body-2 text-medium-emphasis">{{ c.label }}</div>
          </div>
        </v-card>
      </v-col>
    </v-row>

    <v-card class="pa-6">
      <div class="text-subtitle-1 font-weight-medium mb-2">{{ t('dashboard.attention') }}</div>
      <v-list v-if="attention.length" density="comfortable">
        <v-list-item v-for="m in attention" :key="m.id" :to="`/monitors/${m.id}`">
          <template #prepend>
            <StatusChip :state="m.state" size="small" />
          </template>
          <v-list-item-title class="font-weight-medium">{{ m.name }}</v-list-item-title>
          <v-list-item-subtitle>
            {{ m.source_type }}
            <span class="mx-2">·</span>
            <VersionDiff :old-version="m.current_version" :new-version="m.latest_version" />
            <v-chip v-if="m.compare_state === 'lagging'" size="x-small" color="error" class="ms-2" label>
              {{ t('compare.chip.lagging', { ref: m.reference_monitor_name || m.compare_ref_version || t('compare.upstream') }) }}
            </v-chip>
            <span v-if="m.last_error" class="text-error"> {{ m.last_error }}</span>
          </v-list-item-subtitle>
          <template #append>
            <span class="text-caption text-medium-emphasis">
              <RelativeTime :time="m.last_change_at ?? m.last_checked_at" />
            </span>
          </template>
        </v-list-item>
      </v-list>
      <div v-else class="text-medium-emphasis py-4 text-center">{{ t('dashboard.allGood') }}</div>
    </v-card>
  </div>
  <div v-else class="text-center py-12">
    <v-progress-circular indeterminate />
  </div>
</template>
