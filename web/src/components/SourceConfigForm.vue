<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { FieldSpec, SourceMeta } from '@/api/types'
import { getSourceTypes } from '@/api/monitors'
import { useMetaText } from '@/composables/useMetaText'

const props = defineProps<{
  modelValue: Record<string, unknown>
  sourceType: string
}>()
const emit = defineEmits<{
  'update:modelValue': [Record<string, unknown>]
  'update:sourceType': [string]
}>()

const { t } = useI18n()
const { label, help, placeholder } = useMetaText()

const types = ref<SourceMeta[]>([])
const current = computed(() => types.value.find(t => t.type === props.sourceType))
const fields = computed<FieldSpec[]>(() => current.value?.fields ?? [])
const typeOptions = computed(() => types.value.map(x => ({ value: x.type, title: label(x) })))

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
  if (f.required) {
    rules.push(v => (v !== undefined && v !== null && String(v).trim() !== '') || t('common.requiredField', { field: label(f) }))
  }
  return rules
}
</script>

<template>
  <v-select
    :model-value="sourceType"
    :items="typeOptions"
    item-title="title"
    item-value="value"
    :label="t('monitors.edit.sourceType')"
    :prepend-inner-icon="current?.icon"
    @update:model-value="$emit('update:sourceType', $event)"
  />

  <template v-for="f in fields" :key="f.key">
    <v-switch
      v-if="f.type === 'bool'"
      :model-value="!!modelValue[f.key]"
      :label="label(f)"
      :hint="help(f)"
      color="primary"
      persistent-hint
      @update:model-value="setField(f.key, $event)"
    />

    <v-select
      v-else-if="f.type === 'select'"
      :model-value="(modelValue[f.key] as string) ?? ''"
      :items="f.options"
      :label="label(f)"
      :hint="help(f)"
      persistent-hint
      @update:model-value="setField(f.key, $event)"
    />

    <v-text-field
      v-else
      :model-value="(modelValue[f.key] as string) ?? ''"
      :label="label(f) + (f.required ? ' *' : '')"
      :hint="help(f)"
      :placeholder="placeholder(f)"
      :rules="fieldRules(f)"
      :type="f.type === 'secret' ? 'password' : 'text'"
      persistent-hint
      @update:model-value="setField(f.key, $event)"
    />
  </template>
</template>
