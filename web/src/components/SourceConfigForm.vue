<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
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

// 切换类型时保留同名字段值。不走 watch(sourceType)：编辑页 getMonitor 回填
// source_type 也会触发 watch，而此时类型列表可能尚未加载（nextFields=[]），
// 会把已加载的 source_config 竞态清空。改为仅由用户在类型选择器上显式触发。
function onTypeChange(next: string) {
  emit('update:sourceType', next)
  const nextFields = types.value.find(t => t.type === next)?.fields
  if (!next || next === props.sourceType || !nextFields) return
  const keep: Record<string, unknown> = {}
  for (const f of nextFields) {
    if (f.key in props.modelValue) keep[f.key] = props.modelValue[f.key]
    else if (f.default !== undefined) keep[f.key] = f.default
  }
  emit('update:modelValue', keep)
}

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
    @update:model-value="onTypeChange"
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
