<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { getStats } from '@/api/monitors'
import { listMonitors } from '@/api/monitors'
import StatusChip from '@/components/StatusChip.vue'
import RelativeTime from '@/components/RelativeTime.vue'
import VersionDiff from '@/components/VersionDiff.vue'
import type { Monitor } from '@/api/types'

const stats = ref<{ total: number; has_update: number; error: number; ok: number; missing: number; unknown: number } | null>(null)
const attention = ref<Monitor[]>([])

onMounted(async () => {
  stats.value = await getStats()
  // 需要关注：has_update + error，最多 5 条
  const [upd, err] = await Promise.all([
    listMonitors({ state: 'has_update', per_page: 5 }),
    listMonitors({ state: 'error', per_page: 5 }),
  ])
  attention.value = [...upd.items, ...err.items].slice(0, 5)
})

const cards = [
  { key: 'total', label: '监控总数', color: 'primary', icon: 'mdi-radar', to: '/monitors' },
  { key: 'has_update', label: '有更新', color: 'warning', icon: 'mdi-arrow-up-circle', to: '/monitors?state=has_update' },
  { key: 'error', label: '异常', color: 'error', icon: 'mdi-alert-circle', to: '/monitors?state=error' },
  { key: 'ok', label: '正常运行', color: 'success', icon: 'mdi-check-circle', to: '/monitors?state=up_to_date' },
] as const
</script>

<template>
  <div v-if="stats">
    <div class="text-h5 mb-6">仪表盘</div>

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
      <div class="text-subtitle-1 font-weight-medium mb-2">需要关注</div>
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
            <span v-if="m.last_error" class="text-error"> {{ m.last_error }}</span>
          </v-list-item-subtitle>
          <template #append>
            <span class="text-caption text-medium-emphasis">
              <RelativeTime :time="m.last_change_at ?? m.last_checked_at" />
            </span>
          </template>
        </v-list-item>
      </v-list>
      <div v-else class="text-medium-emphasis py-4 text-center">一切正常，暂无需要关注的监控</div>
    </v-card>
  </div>
  <div v-else class="text-center py-12">
    <v-progress-circular indeterminate />
  </div>
</template>
