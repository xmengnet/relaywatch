<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import type { FieldSpec, SourceMeta } from '@/api/types'
import { getSourceTypes } from '@/api/monitors'

const props = defineProps<{
  modelValue: Record<string, unknown>
  sourceType: string
}>()
const emit = defineEmits<{
  'update:modelValue': [Record<string, unknown>]
  'update:sourceType': [string]
}>()

const types = ref<SourceMeta[]>([])
const current = computed(() => types.value.find(t => t.type === props.sourceType))
const fields = computed<FieldSpec[]>(() => current.value?.fields ?? [])

onMounted(async () => {
  types.value = (await getSourceTypes()).items
})

// 切换类型时保留同名字段值
watch(() => props.sourceType, (next, prev) => {
  if (!prev || !next) return
  const nextFields = types.value.find(t => t.type === next)?.fields ?? []
  const keep: Record<string, unknown> = {}
  for (const f of nextFields) {
    if (f.key in props.modelValue) keep[f.key] = props.modelValue[f.key]
    else if (f.default !== undefined) keep[f.key] = f.default
  }
  emit('update:modelValue', keep)
})

function setField(key: string, value: unknown) {
  emit('update:modelValue', { ...props.modelValue, [key]: value })
}

function fieldRules(f: FieldSpec) {
  const rules: ((v: unknown) => true | string)[] = []
  if (f.required) rules.push(v => (v !== undefined && v !== null && String(v).trim() !== '') || `${f.label}不能为空`)
  return rules
}
</script>

<template>
  <v-select
    :model-value="sourceType"
    :items="types"
    item-title="label"
    item-value="type"
    label="监控源类型"
    :prepend-inner-icon="current?.icon"
    @update:model-value="$emit('update:sourceType', $event)"
  />

  <template v-for="f in fields" :key="f.key">
    <v-switch
      v-if="f.type === 'bool'"
      :model-value="!!modelValue[f.key]"
      :label="f.label"
      :hint="f.help"
      color="primary"
      persistent-hint
      @update:model-value="setField(f.key, $event)"
    />

    <v-select
      v-else-if="f.type === 'select'"
      :model-value="(modelValue[f.key] as string) ?? ''"
      :items="f.options"
      :label="f.label"
      :hint="f.help"
      persistent-hint
      @update:model-value="setField(f.key, $event)"
    />

    <v-text-field
      v-else
      :model-value="(modelValue[f.key] as string) ?? ''"
      :label="f.label + (f.required ? ' *' : '')"
      :hint="f.help"
      :placeholder="f.placeholder"
      :rules="fieldRules(f)"
      :type="f.type === 'secret' ? 'password' : 'text'"
      persistent-hint
      @update:model-value="setField(f.key, $event)"
    />
  </template>
</template>
