<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { Monitor, MonitorState } from '@/api/types'
import { listMonitors, batchMonitors, checkNow } from '@/api/monitors'
import StatusChip from '@/components/StatusChip.vue'
import RelativeTime from '@/components/RelativeTime.vue'
import VersionDiff from '@/components/VersionDiff.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const loading = ref(true)
const items = ref<Monitor[]>([])
const total = ref(0)
const page = ref(1)
const itemsPerPage = ref(25)
const sortBy = ref<Array<{ key: string; order: 'asc' | 'desc' }>>([])
const search = ref('')
const stateFilter = ref<MonitorState | ''>('')
const selected = ref<Monitor[]>([])
const checkingIds = ref<Set<number>>(new Set())

const stateOptions = [
  { title: '全部状态', value: '' },
  { title: '正常', value: 'up_to_date' },
  { title: '有更新', value: 'has_update' },
  { title: '异常', value: 'error' },
  { title: '已消失', value: 'missing' },
  { title: '未知', value: 'unknown' },
]

const headers = [
  { title: '名称', key: 'name', sortable: true },
  { title: '源', key: 'source_type', sortable: true },
  { title: '当前', key: 'current_version', sortable: false },
  { title: '最新', key: 'latest_version', sortable: false },
  { title: '状态', key: 'state', sortable: true },
  { title: '上次检查', key: 'last_checked_at', sortable: true },
  { title: '操作', key: 'actions', sortable: false, align: 'end' as const },
]

async function load() {
  loading.value = true
  try {
    const r = await listMonitors({
      page: page.value,
      per_page: itemsPerPage.value,
      state: stateFilter.value || undefined,
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

// URL 同步：?state=&q=
watch(() => route.query, (q) => {
  stateFilter.value = (q.state as MonitorState) || ''
  search.value = (q.q as string) || ''
}, { immediate: true })

watch([stateFilter, search], () => {
  router.replace({ query: {
    state: stateFilter.value || undefined,
    q: search.value || undefined,
  } })
  page.value = 1
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
  if (action === 'delete' && !window.confirm(`删除选中的 ${ids.length} 条监控？不可恢复。`)) return
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
      <v-btn color="primary" prepend-icon="mdi-plus" to="/monitors/new">{{ t('monitors.new') }}</v-btn>
    </div>

    <div class="d-flex flex-wrap ga-2 mb-4">
      <v-select
        v-model="stateFilter"
        :items="stateOptions"
        item-title="title"
        item-value="value"
        label="状态"
        density="comfortable"
        hide-details
        style="max-width: 160px"
      />
      <v-text-field
        v-model="search"
        label="搜索名称"
        prepend-inner-icon="mdi-magnify"
        density="comfortable"
        hide-details
        clearable
        style="max-width: 280px"
      />
      <v-spacer />
      <template v-if="hasSelected">
        <span class="text-medium-emphasis align-self-center me-2">已选 {{ selected.length }} 项</span>
        <v-btn height="48" @click="doBatch('enable')">启用</v-btn>
        <v-btn height="48" @click="doBatch('disable')">停用</v-btn>
        <v-btn height="48" color="error" @click="doBatch('delete')">删除</v-btn>
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
        <v-chip size="small" variant="tonal" label>{{ item.source_type }}</v-chip>
      </template>
      <template #item.current_version="{ item }">
        <code>{{ item.current_version || '—' }}</code>
      </template>
      <template #item.latest_version="{ item }">
        <VersionDiff :old-version="item.current_version" :new-version="item.latest_version" />
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
          aria-label="立即检查"
          @click="rowCheck(item)"
        />
        <v-btn icon="mdi-pencil" size="small" variant="text" :to="`/monitors/${item.id}/edit`" aria-label="编辑" />
      </template>
      <template #no-data>
        <div class="py-8">
          <div class="text-h6 mb-2">{{ t('monitors.empty') }}</div>
          <div class="text-medium-emphasis mb-4">{{ t('monitors.emptyHint') }}</div>
          <v-btn color="primary" prepend-icon="mdi-plus" to="/monitors/new">{{ t('monitors.addFirst') }}</v-btn>
        </div>
      </template>
    </v-data-table-server>
  </div>
</template>
