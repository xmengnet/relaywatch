<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'

const props = defineProps<{ time?: string | null }>()

const display = ref('')
const absolute = computed(() => (props.time ? new Date(props.time).toLocaleString() : ''))

function update() {
  if (!props.time) {
    display.value = '—'
    return
  }
  const diff = Date.now() - new Date(props.time).getTime()
  const min = Math.floor(diff / 60000)
  if (min < 1) display.value = '刚刚'
  else if (min < 60) display.value = `${min} 分钟前`
  else if (min < 1440) display.value = `${Math.floor(min / 60)} 小时前`
  else display.value = `${Math.floor(min / 1440)} 天前`
}

let timer: number | undefined
onMounted(() => {
  update()
  timer = window.setInterval(update, 30000)
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <v-tooltip :text="absolute" location="top">
    <template #activator="{ props: p }">
      <span v-bind="p">{{ display }}</span>
    </template>
  </v-tooltip>
</template>
