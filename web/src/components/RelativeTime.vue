<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useFormat } from '@/composables/useFormat'

const props = defineProps<{ time?: string | null }>()

const { t, locale } = useI18n()
const { formatDateTime } = useFormat()

const display = ref('')
const absolute = computed(() => formatDateTime(props.time))

function update() {
  if (!props.time) {
    display.value = '—'
    return
  }
  const diff = Date.now() - new Date(props.time).getTime()
  const min = Math.floor(diff / 60000)
  if (min < 1) display.value = t('time.justNow')
  else if (min < 60) display.value = t('time.minutesAgo', { n: min })
  else if (min < 1440) display.value = t('time.hoursAgo', { n: Math.floor(min / 60) })
  else display.value = t('time.daysAgo', { n: Math.floor(min / 1440) })
}

let timer: number | undefined
onMounted(() => {
  update()
  timer = window.setInterval(update, 30000)
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
})
watch(locale, update)
</script>

<template>
  <v-tooltip :text="absolute" location="top">
    <template #activator="{ props: p }">
      <span v-bind="p">{{ display }}</span>
    </template>
  </v-tooltip>
</template>
