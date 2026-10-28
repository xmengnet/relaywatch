<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useTheme } from '@/composables/useTheme'
import { useLocale } from '@/composables/useLocale'

useTheme()
const { t } = useI18n()
const { locale, setLocale, localeOptions } = useLocale()
</script>

<template>
  <v-app>
    <div class="locale-switch">
      <v-menu location="bottom end">
        <template #activator="{ props }">
          <v-btn
            v-bind="props"
            variant="text"
            size="small"
            prepend-icon="mdi-translate"
            :aria-label="t('common.language')"
          >
            {{ locale === 'en' ? 'English' : '简体中文' }}
          </v-btn>
        </template>
        <v-list density="compact">
          <v-list-item
            v-for="o in localeOptions"
            :key="o.value"
            :active="locale === o.value"
            @click="setLocale(o.value)"
          >
            <v-list-item-title>{{ o.title }}</v-list-item-title>
          </v-list-item>
        </v-list>
      </v-menu>
    </div>
    <v-main class="d-flex align-center justify-center" style="min-height: 100vh">
      <router-view />
    </v-main>
  </v-app>
</template>

<style scoped>
.locale-switch {
  position: fixed;
  top: 12px;
  right: 16px;
  z-index: 10;
}
</style>
