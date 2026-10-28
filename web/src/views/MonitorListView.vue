<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { Monitor, MonitorState } from '@/api/types'
import { listMonitors, batchMonitors, checkNow } from '@/api/monitors'
import { useAuthStore } from '@/stores/auth'
import { useSse } from '@/composables/useSse'
import { useSourceTypes } from '@/composables/useSourceTypes'
import { useMetaText } from '@/composables/useMetaText'
import StatusChip from '@/components/StatusChip.vue'
import RelativeTime from '@/components/RelativeTime.vue'
import VersionDiff from '@/components/VersionDiff.vue'

const { t } = useI18n()
const { label: metaLabel } = useMetaText()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const loading = ref(true)
const items = ref<Monitor[]>([])
const total = ref(0)
const page = ref(1)
const itemsPerPage = ref(25)
const sortBy = ref<Array<{ key: string; order: 'asc' | 'desc' }>>([])
const search = ref('')
const stateFilter = ref<MonitorState | ''>('')
const compareFilter = ref('')
const selected = ref<Monitor[]>([])
const checkingIds = ref<Set<number>>(new Set())

const { ensure: ensureSourceTypes, meta: sourceMeta } = useSourceTypes()
onMounted(() => ensureSourceTypes())

function refName(item: Monitor): string {
  return item.reference_monitor_name || `#${item.reference_monitor_id}`
}

const stateOptions = computed(() => [
  { title: t('monitors.allStates'), value: '' },
  { title: t('status.up_to_date'), value: 'up_to_date' },
  { title: t('status.has_update'), value: 'has_update' },
  { title: t('status.error'), value: 'error' },
  { title: t('status.missing'), value: 'missing' },
  { title: t('status.unknown'), value: 'unknown' },
])

const compareOptions = computed(() => [
  { title: t('monitors.allCompare'), value: '' },
  { title: t('compare.lagging'), value: 'lagging' },
  { title: t('compare.in_sync'), value: 'in_sync' },
  { title: t('compare.ahead'), value: 'ahead' },
])

const headers = computed(() => {
  const cols: Array<{ title: string; key: string; sortable: boolean; align?: 'end' }> = [
    { title: t('monitors.headers.name'), key: 'name', sortable: true },
    { title: t('monitors.headers.source'), key: 'source_type', sortable: true },
  ]
  if (auth.user?.role === 'admin') {
    cols.push({ title: t('monitors.headers.owner'), key: 'owner_name', sortable: false })
  }
  cols.push(
    { title: t('monitors.headers.current'), key: 'current_version', sortable: false },
    { title: t('monitors.headers.latest'), key: 'latest_version', sortable: false },
    { title: t('monitors.headers.compare'), key: 'compare_state', sortable: false },
    { title: t('monitors.headers.state'), key: 'state', sortable: true },
    { title: t('monitors.headers.lastChecked'), key: 'last_checked_at', sortable: true },
    { title: t('monitors.headers.actions'), key: 'actions', sortable: false, align: 'end' },
  )
  return cols
})

async function load() {
  loading.value = true
  try {
    const r = await listMonitors({
      page: page.value,
      per_page: itemsPerPage.value,
      state: stateFilter.value || undefined,
      compare: compareFilter.value || undefined,
      q: search.value || undefined,
      sort: sortBy.value[0]?.key,
      order: sortBy.value[0]?.order,
    })
    items.value = r.items
    total.value = r.total
  } finally {
    loading.value = false
  }
}

// URL 同步：?state=&compare=&q=
watch(() => route.query, (q) => {
  stateFilter.value = (q.state as MonitorState) || ''
  compareFilter.value = (q.compare as string) || ''
  search.value = (q.q as string) || ''
}, { immediate: true })

watch([stateFilter, compareFilter, search], () => {
  router.replace({ query: {
    state: stateFilter.value || undefined,
    compare: compareFilter.value || undefined,
    q: search.value || undefined,
  } })
  page.value = 1
  load()
})

// SSE 实时更新：后台调度或手动检查完成时实时刷新列表
useSse('check.done', () => {
  load()
})

function onUpdateOptions(opts: { page?: number; itemsPerPage?: number; sortBy?: Array<{ key: string; order: 'asc' | 'desc' }> }) {
  if (opts.page) page.value = opts.page
  if (opts.itemsPerPage) itemsPerPage.value = opts.itemsPerPage
  if (opts.sortBy) sortBy.value = opts.sortBy
  load()
}

async function doBatch(action: string) {
  const ids = selected.value.map(m => m.id)
  if (!ids.length) return
  if (action === 'delete' && !window.confirm(t('monitors.deleteConfirm', { n: ids.length }))) return
  await batchMonitors(action, ids)
  selected.value = []
  await load()
}

async function rowCheck(m: Monitor) {
  checkingIds.value.add(m.id)
  try {
    await checkNow(m.id)
    await load()
  } finally {
    checkingIds.value.delete(m.id)
  }
}

const hasSelected = computed(() => selected.value.length > 0)
</script>

<template>
  <div>
    <div class="d-flex align-center flex-wrap ga-2 mb-4">
      <div class="text-h5">{{ t('monitors.title') }}</div>
      <v-spacer />
      <v-btn v-if="auth.can('monitor.create')" color="primary" prepend-icon="mdi-plus" to="/monitors/new">{{ t('monitors.new') }}</v-btn>
    </div>

    <div class="d-flex flex-wrap ga-2 mb-4">
      <v-select
        v-model="stateFilter"
        :items="stateOptions"
        item-title="title"
        item-value="value"
        :label="t('monitors.stateLabel')"
        density="comfortable"
        hide-details
        style="max-width: 140px"
      />
      <v-select
        v-model="compareFilter"
        :items="compareOptions"
        item-title="title"
        item-value="value"
        :label="t('monitors.compareLabel')"
        density="comfortable"
        hide-details
        style="max-width: 160px"
      />
      <v-text-field
        v-model="search"
        :label="t('monitors.searchLabel')"
        prepend-inner-icon="mdi-magnify"
        density="comfortable"
        hide-details
        clearable
        style="max-width: 280px"
      />
      <v-spacer />
      <template v-if="hasSelected">
        <span class="text-medium-emphasis align-self-center me-2">{{ t('monitors.selectedCount', { n: selected.length }) }}</span>
        <v-btn height="48" variant="tonal" prepend-icon="mdi-play" @click="doBatch('enable')">{{ t('monitors.enable') }}</v-btn>
        <v-btn height="48" variant="tonal" prepend-icon="mdi-pause" @click="doBatch('disable')">{{ t('monitors.disable') }}</v-btn>
        <v-btn height="48" color="error" prepend-icon="mdi-delete" @click="doBatch('delete')">{{ t('common.delete') }}</v-btn>
      </template>
    </div>

    <v-data-table-server
      v-model:items-per-page="itemsPerPage"
      v-model:page="page"
      v-model:sort-by="sortBy"
      v-model="selected"
      :headers="headers"
      :items="items"
      :items-length="total"
      :loading="loading"
      :items-per-page-options="[10, 25, 50]"
      item-value="id"
      show-select
      return-object
      @update:options="onUpdateOptions"
    >
      <template #item.name="{ item }">
        <router-link :to="`/monitors/${item.id}`" class="text-decoration-none text-high-emphasis font-weight-medium">
          {{ item.name }}
        </router-link>
        <div v-if="item.tags?.length" class="mt-1">
          <v-chip v-for="tg in item.tags.slice(0, 3)" :key="tg" size="x-small" label class="me-1">{{ tg }}</v-chip>
        </div>
      </template>
      <template #item.source_type="{ item }">
        <v-tooltip :text="sourceMeta(item.source_type) ? metaLabel(sourceMeta(item.source_type)!) : item.source_type" location="top">
          <template #activator="{ props: tooltipProps }">
            <span v-bind="tooltipProps" class="d-inline-flex align-center ga-2 cursor-default">
              <v-icon :icon="sourceMeta(item.source_type)?.icon || 'mdi-radar-outline'" size="20" />
              <span class="text-medium-emphasis text-body-2">{{ item.source_type }}</span>
            </span>
          </template>
        </v-tooltip>
      </template>
      <template v-if="auth.user?.role === 'admin'" #item.owner_name="{ item }">
        <span class="text-medium-emphasis text-body-2">{{ item.owner_name || `#${item.owner_id ?? '—'}` }}</span>
      </template>
      <template #item.current_version="{ item }">
        <code>{{ item.current_version || '—' }}</code>
      </template>
      <template #item.latest_version="{ item }">
        <VersionDiff :old-version="item.current_version" :new-version="item.latest_version" />
      </template>
      <template #item.compare_state="{ item }">
        <template v-if="item.reference_monitor_id">
          <v-tooltip
            :text="t('compare.tooltip', { ref: refName(item), local: item.current_version || '—', refVersion: item.compare_ref_version || '—' })"
            location="top"
          >
            <template #activator="{ props: tooltipProps }">
              <span v-bind="tooltipProps" class="cursor-default">
                <v-chip v-if="item.compare_state === 'lagging'" size="small" color="error" variant="flat" label>
                  {{ t('compare.chip.lagging', { ref: refName(item) }) }}
                </v-chip>
                <v-chip v-else-if="item.compare_state === 'in_sync'" size="small" color="success" variant="tonal" label>
                  {{ t('compare.chip.in_sync', { ref: refName(item) }) }}
                </v-chip>
                <v-chip v-else-if="item.compare_state === 'ahead'" size="small" color="purple" variant="tonal" label>
                  {{ t('compare.chip.ahead', { ref: refName(item) }) }}
                </v-chip>
                <v-chip v-else size="small" variant="text" label class="text-medium-emphasis">
                  {{ t('compare.chip.pending', { ref: refName(item) }) }}
                </v-chip>
              </span>
            </template>
          </v-tooltip>
        </template>
        <span v-else class="text-disabled">—</span>
      </template>
      <template #item.state="{ item }">
        <StatusChip :state="item.state" size="small" />
      </template>
      <template #item.last_checked_at="{ item }">
        <RelativeTime :time="item.last_checked_at" />
      </template>
      <template #item.actions="{ item }">
        <v-btn
          icon="mdi-radar"
          size="small"
          variant="text"
          :loading="checkingIds.has(item.id)"
          :aria-label="t('monitors.checkNow')"
          @click="rowCheck(item)"
        />
        <v-btn icon="mdi-pencil" size="small" variant="text" :to="`/monitors/${item.id}/edit`" :aria-label="t('common.edit')" />
      </template>
      <template #no-data>
        <div class="py-8">
          <div class="text-h6 mb-2">{{ t('monitors.empty') }}</div>
          <div class="text-medium-emphasis mb-4">{{ t('monitors.emptyHint') }}</div>
          <v-btn v-if="auth.can('monitor.create')" color="primary" prepend-icon="mdi-plus" to="/monitors/new">{{ t('monitors.addFirst') }}</v-btn>
        </div>
      </template>
    </v-data-table-server>
  </div>
</template>
