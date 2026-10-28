<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { exportUrl } from '@/api/import'
import logoUrl from '@/assets/logo.svg'

const { t } = useI18n()

function downloadExport(redact: boolean) {
  // API 需要会话 Cookie，直接用 <a> 下载
  const a = document.createElement('a')
  a.href = exportUrl(redact)
  a.download = redact ? 'relaywatch-export.json' : 'relaywatch-export-full.json'
  a.click()
}
</script>

<template>
  <div>
    <div class="text-h5 mb-4">{{ t('settings.title') }}</div>

    <v-card class="pa-6 mb-4" max-width="860">
      <div class="text-subtitle-1 font-weight-medium mb-2">{{ t('settings.export') }}</div>
      <div class="text-body-2 text-medium-emphasis mb-4">{{ t('settings.exportDesc') }}</div>
      <div class="d-flex ga-2">
        <v-btn prepend-icon="mdi-download" @click="downloadExport(true)">{{ t('settings.exportRedacted') }}</v-btn>
        <v-btn prepend-icon="mdi-download-box" variant="tonal" @click="downloadExport(false)">{{ t('settings.exportFull') }}</v-btn>
      </div>
    </v-card>

    <v-card class="pa-6" max-width="860">
      <div class="d-flex align-center mb-3">
        <img :src="logoUrl" alt="RelayWatch logo" width="44" height="44" class="rounded-lg me-3" />
        <div>
          <div class="text-subtitle-1 font-weight-medium">RelayWatch</div>
          <div class="text-caption text-medium-emphasis">{{ t('settings.aboutTagline') }}</div>
        </div>
      </div>
      <div class="text-body-2 text-medium-emphasis">
        {{ t('settings.aboutDesc') }}
      </div>
    </v-card>
  </div>
</template>
