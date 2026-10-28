<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { listDeliveries, resendDelivery, type Delivery } from '@/api/channels'
import RelativeTime from '@/components/RelativeTime.vue'

const { t } = useI18n()

const loading = ref(true)
const items = ref<Delivery[]>([])
const total = ref(0)
const page = ref(1)
const perPage = ref(25)
const statusFilter = ref('')
const snackbar = ref(false)
const snackbarText = ref('')
const snackbarColor = ref('success')

const statusOptions = computed(() => [
  { title: t('notifications.all'), value: '' },
  { title: t('notifications.statuses.pending'), value: 'pending' },
  { title: t('notifications.statuses.sent'), value: 'sent' },
  { title: t('notifications.statuses.failed'), value: 'failed' },
  { title: t('notifications.statuses.deferred'), value: 'deferred' },
  { title: t('notifications.statuses.canceled'), value: 'canceled' },
])

const statusMeta = computed<Record<string, { color: string; text: string }>>(() => ({
  pending: { color: 'info', text: t('notifications.statuses.pending') },
  sent: { color: 'success', text: t('notifications.statuses.sent') },
  failed: { color: 'error', text: t('notifications.statuses.failed') },
  deferred: { color: 'warning', text: t('notifications.statuses.deferred') },
  canceled: { color: 'default', text: t('notifications.statuses.canceled') },
}))

const kindMeta = computed<Record<string, { color: string; text: string }>>(() => ({
  update: { color: 'primary', text: t('notifications.kinds.update') },
  compare: { color: 'deep-purple', text: t('notifications.kinds.compare') },
}))

const headers = computed<Array<{ title: string; key: string; sortable: boolean; align?: 'end' }>>(() => [
  { title: t('notifications.headers.time'), key: 'created_at', sortable: false },
  { title: t('notifications.headers.monitor'), key: 'monitor_id', sortable: false },
  { title: t('notifications.headers.event'), key: 'kind', sortable: false },
  { title: t('notifications.headers.channel'), key: 'channel_id', sortable: false },
  { title: t('notifications.headers.status'), key: 'status', sortable: false },
  { title: t('notifications.headers.error'), key: 'last_error', sortable: false },
  { title: t('notifications.headers.actions'), key: 'actions', sortable: false, align: 'end' },
])

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
    snackbarText.value = r.status === 'sent'
      ? t('notifications.resendSuccess')
      : t('notifications.resendResult', { status: r.status, error: r.last_error ?? '' })
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
      <div class="text-h5">{{ t('notifications.title') }}</div>
      <v-spacer />
      <v-select
        v-model="statusFilter"
        :items="statusOptions"
        item-title="title"
        item-value="value"
        :label="t('notifications.statusLabel')"
        density="comfortable"
        hide-details
        style="max-width: 160px"
      />
    </div>

    <v-data-table-server
      v-model:items-per-page="perPage"
      v-model:page="page"
      :headers="headers"
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
        >{{ t('notifications.resend') }}</v-btn>
      </template>
      <template #no-data>
        <div class="text-medium-emphasis py-6">{{ t('notifications.empty') }}</div>
      </template>
    </v-data-table-server>

    <v-snackbar v-model="snackbar" :color="snackbarColor" location="top right" :timeout="4000">
      {{ snackbarText }}
    </v-snackbar>
  </div>
</template>
