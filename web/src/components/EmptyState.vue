<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{ to?: string }>()
const { t } = useI18n()
</script>

<template>
  <v-empty-state
    icon="mdi-radar"
    :headline="t('monitors.empty')"
    :text="t('monitors.emptyHint')"
  >
    <template #actions>
      <v-btn v-if="to" :to="to" color="primary" prepend-icon="mdi-plus">{{ t('monitors.addFirst') }}</v-btn>
    </template>
  </v-empty-state>
</template>
