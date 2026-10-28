<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { MirrorRule } from '@/api/site'

// placeholder 走 script 常量：字面量 URL 中的 // 会触发 vue-tsc 3.3 模板表达式解析缺陷
const fromPlaceholder = 'https://api.github.com'
const toPlaceholder = 'https://proxy.example/api.github.com'

const props = defineProps<{ modelValue: MirrorRule[] }>()
const { t } = useI18n()

const rows = computed(() => props.modelValue)
const emit = defineEmits<{ (e: 'update:modelValue', v: MirrorRule[]): void }>()

function update(i: number, key: 'from' | 'to', value: string) {
  const next = props.modelValue.map((r, j) => (j === i ? { ...r, [key]: value } : r))
  emit('update:modelValue', next)
}

function remove(i: number) {
  emit('update:modelValue', props.modelValue.filter((_, j) => j !== i))
}
</script>

<template>
  <div>
    <div v-for="(row, i) in rows" :key="i" class="d-flex align-center ga-2 mb-2">
      <v-text-field
        :model-value="row.from" :label="t('site.mirrorFrom')" :placeholder="fromPlaceholder"
        hide-details density="compact" class="flex-grow-1"
        @update:model-value="update(i, 'from', $event)"
      />
      <v-icon icon="mdi-arrow-right" size="20" class="text-medium-emphasis" />
      <v-text-field
        :model-value="row.to" :label="t('site.mirrorTo')" :placeholder="toPlaceholder"
        hide-details density="compact" class="flex-grow-1"
        @update:model-value="update(i, 'to', $event)"
      />
      <v-btn icon="mdi-close" size="small" variant="text" color="error" :aria-label="t('common.remove')" @click="remove(i)" />
    </div>
  </div>
</template>
