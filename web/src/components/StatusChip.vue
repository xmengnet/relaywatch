<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { MonitorState } from '@/api/types'

const props = defineProps<{ state: MonitorState; size?: 'small' | 'default' | 'x-small' }>()

const { t } = useI18n()

const map = {
  up_to_date: { color: 'success', icon: 'mdi-check-circle' },
  has_update: { color: 'warning', icon: 'mdi-arrow-up-circle' },
  error: { color: 'error', icon: 'mdi-alert-circle' },
  missing: { color: 'error', icon: 'mdi-cloud-off-outline' },
  unknown: { color: 'default', icon: 'mdi-help-circle' },
} as const

const meta = computed(() => map[props.state] ?? map.unknown)
</script>

<template>
  <v-chip :color="meta.color" :size="size ?? 'default'" :prepend-icon="meta.icon" variant="tonal">
    {{ t(`status.${state}`) }}
  </v-chip>
</template>
