<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
</script>

<template>
  <v-empty-state icon="mdi-help-circle-outline" :headline="t('notFound.title')" :text="t('notFound.hint')">
    <template #actions>
      <v-btn to="/" prepend-icon="mdi-home">{{ t('notFound.back') }}</v-btn>
    </template>
  </v-empty-state>
</template>
