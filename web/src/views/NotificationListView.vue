<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { listDeliveries, resendDelivery, type Delivery } from '@/api/channels'
import RelativeTime from '@/components/RelativeTime.vue'

const loading = ref(true)
const items = ref<Delivery[]>([])
const total = ref(0)
const page = ref(1)
const perPage = ref(25)
const statusFilter = ref('')
const snackbar = ref(false)
const snackbarText = ref('')
const snackbarColor = ref('success')

const statusOptions = [
  { title: '全部', value: '' },
  { title: '待投递', value: 'pending' },
  { title: '已送达', value: 'sent' },
  { title: '失败', value: 'failed' },
  { title: '已推迟', value: 'deferred' },
  { title: '已取消', value: 'canceled' },
]

const statusMeta: Record<string, { color: string; text: string }> = {
  pending: { color: 'info', text: '待投递' },
  sent: { color: 'success', text: '已送达' },
  failed: { color: 'error', text: '失败' },
  deferred: { color: 'warning', text: '已推迟' },
  canceled: { color: 'default', text: '已取消' },
}

const kindMeta: Record<string, { color: string; text: string }> = {
  update: { color: 'primary', text: '版本更新' },
  compare: { color: 'deep-purple', text: '版本对比' },
}

async function load() {
  loading.value = true
  try {
    const r = await listDeliveries({
      page: page.value,
      per_page: perPage.value,
      status: statusFilter.value || undefined,
    })
    items.value = r.items
    total.value = r.total
  } finally {
    loading.value = false
  }
}

onMounted(load)
watch([statusFilter], () => { page.value = 1; load() })

async function doResend(d: Delivery) {
  try {
    const r = await resendDelivery(d.id)
    snackbarText.value = r.status === 'sent' ? '重发成功' : `重发结果：${r.status} ${r.last_error ?? ''}`
    snackbarColor.value = r.status === 'sent' ? 'success' : 'error'
    snackbar.value = true
    await load()
  } catch (e) {
    snackbarText.value = e instanceof Error ? e.message : String(e)
    snackbarColor.value = 'error'
    snackbar.value = true
  }
}

function canResend(d: Delivery) {
  return d.status === 'sent' || d.status === 'failed' || d.status === 'canceled'
}
</script>

<template>
  <div>
    <div class="d-flex align-center flex-wrap ga-2 mb-4">
      <div class="text-h5">通知历史</div>
      <v-spacer />
      <v-select
        v-model="statusFilter"
        :items="statusOptions"
        item-title="title"
        item-value="value"
        label="状态"
        density="comfortable"
        hide-details
        style="max-width: 160px"
      />
    </div>

    <v-data-table-server
      v-model:items-per-page="perPage"
      v-model:page="page"
      :headers="[
        { title: '时间', key: 'created_at', sortable: false },
        { title: '监控', key: 'monitor_id', sortable: false },
        { title: '事件', key: 'kind', sortable: false },
        { title: '渠道', key: 'channel_id', sortable: false },
        { title: '状态', key: 'status', sortable: false },
        { title: '错误', key: 'last_error', sortable: false },
        { title: '操作', key: 'actions', sortable: false, align: 'end' },
      ]"
      :items="items"
      :items-length="total"
      :loading="loading"
      item-value="id"
      @update:options="(o: { page?: number; itemsPerPage?: number }) => { if (o.page) page = o.page; if (o.itemsPerPage) perPage = o.itemsPerPage; load() }"
    >
      <template #item.created_at="{ item }">
        <RelativeTime :time="item.created_at" />
      </template>
      <template #item.monitor_id="{ item }">
        <router-link :to="`/monitors/${item.monitor_id}`" class="text-decoration-none text-high-emphasis">
          {{ item.monitor_name || `#${item.monitor_id}` }}
        </router-link>
      </template>
      <template #item.kind="{ item }">
        <v-chip :color="kindMeta[item.kind]?.color ?? 'default'" size="small" variant="tonal" label>
          {{ kindMeta[item.kind]?.text ?? item.kind }}
        </v-chip>
      </template>
      <template #item.channel_id="{ item }">
        {{ item.channel_name || `#${item.channel_id}` }}
      </template>
      <template #item.status="{ item }">
        <v-chip :color="statusMeta[item.status]?.color ?? 'default'" size="small" variant="tonal">
          {{ statusMeta[item.status]?.text ?? item.status }}
        </v-chip>
        <span v-if="item.attempts > 1" class="text-caption text-medium-emphasis ms-1">×{{ item.attempts }}</span>
      </template>
      <template #item.last_error="{ item }">
        <span class="text-error text-caption">{{ item.last_error || '' }}</span>
      </template>
      <template #item.actions="{ item }">
        <v-btn
          v-if="canResend(item)"
          size="small"
          variant="text"
          prepend-icon="mdi-send-clock"
          @click="doResend(item)"
        >重发</v-btn>
      </template>
      <template #no-data>
        <div class="text-medium-emphasis py-6">暂无投递记录</div>
      </template>
    </v-data-table-server>

    <v-snackbar v-model="snackbar" :color="snackbarColor" location="top right" :timeout="4000">
      {{ snackbarText }}
    </v-snackbar>
  </div>
</template>
