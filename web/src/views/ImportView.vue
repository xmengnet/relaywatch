<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { importPreview, doImport, type ImportPreview } from '@/api/import'

const { t } = useI18n()
const auth = useAuthStore()
const router = useRouter()
const text = ref('')
const preview = ref<ImportPreview | null>(null)
const selected = ref<Set<string>>(new Set())
const parsing = ref(false)
const importing = ref(false)
const errorMsg = ref('')
const snackbar = ref(false)
const snackbarText = ref('')
const snackbarColor = ref('success')

function toggle(name: string) {
  if (selected.value.has(name)) selected.value.delete(name)
  else selected.value.add(name)
}

async function parse() {
  if (!text.value.trim()) return
  parsing.value = true
  preview.value = null
  selected.value = new Set()
  errorMsg.value = ''
  try {
    const r = await importPreview(text.value)
    preview.value = r
    for (const it of r.items) {
      if (it.valid) selected.value.add(it.name)
    }
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e)
  } finally {
    parsing.value = false
  }
}

async function confirmImport() {
  if (!preview.value || selected.value.size === 0) return
  importing.value = true
  try {
    const r = await doImport(text.value, [...selected.value])
    snackbarText.value = t('import.imported', { n: r.imported, m: r.skipped })
    snackbarColor.value = 'success'
    snackbar.value = true
    setTimeout(() => router.push('/monitors'), 800)
  } catch (e) {
    snackbarText.value = e instanceof Error ? e.message : String(e)
    snackbarColor.value = 'error'
    snackbar.value = true
  } finally {
    importing.value = false
  }
}

function onFile(e: Event) {
  const f = (e.target as HTMLInputElement).files?.[0]
  if (!f) return
  const reader = new FileReader()
  reader.onload = () => {
    text.value = String(reader.result ?? '')
    parse()
  }
  reader.readAsText(f)
}
</script>

<template>
  <div>
    <div class="text-h5 mb-4">{{ t('import.title') }}</div>
    <v-alert v-if="!auth.can('import')" type="warning" variant="tonal" max-width="720" class="mb-4">{{ t('import.noPermission') }}</v-alert>
    <v-row>
      <v-col cols="12" md="6">
        <v-card class="pa-6">
          <div class="text-subtitle-1 font-weight-medium mb-2">{{ t('import.keyfile') }}</div>
          <div class="text-body-2 text-medium-emphasis mb-4">{{ t('import.desc') }}</div>
          <v-file-input :label="t('import.upload')" accept=".toml,.txt" density="comfortable" prepend-icon="" prepend-inner-icon="mdi-file-upload-outline" @change="onFile" />
          <v-textarea
            v-model="text"
            :label="t('import.configText')"
            rows="12"
            class="font-mono"
            placeholder='[mygo]
source = "github"
github = "xmengnet/relaywatch"
use_latest_release = true'
          />
          <v-btn color="primary" :loading="parsing" :disabled="!text.trim()" prepend-icon="mdi-text-box-search" @click="parse">{{ t('import.parse') }}</v-btn>
          <v-alert v-if="errorMsg" type="error" density="compact" class="mt-4">{{ errorMsg }}</v-alert>
        </v-card>
      </v-col>

      <v-col cols="12" md="6">
        <v-card v-if="preview" class="pa-6">
          <div class="text-subtitle-1 font-weight-medium mb-2">{{ t('import.preview', { n: preview.items.length }) }}</div>
          <v-list density="compact">
            <v-list-item v-for="it in preview.items" :key="it.name">
              <template #prepend>
                <v-checkbox-btn
                  :model-value="selected.has(it.name)"
                  :disabled="!it.valid"
                  color="primary"
                  @update:model-value="toggle(it.name)"
                />
              </template>
              <v-list-item-title class="font-weight-medium">
                {{ it.name }}
                <v-chip size="x-small" label variant="tonal" class="ms-2">{{ it.source_type }}</v-chip>
                <v-chip v-if="!it.valid" size="x-small" color="error" variant="tonal" class="ms-1">{{ t('import.invalid') }}</v-chip>
              </v-list-item-title>
              <v-list-item-subtitle>
                <code class="text-caption">{{ JSON.stringify(it.source_config) }}</code>
                <div v-if="it.error" class="text-error">{{ it.error }}</div>
              </v-list-item-subtitle>
            </v-list-item>
          </v-list>

          <v-alert v-for="w in preview.warnings" :key="w" type="warning" density="compact" class="mt-2">{{ w }}</v-alert>
          <div v-for="i in preview.ignored" :key="i" class="text-caption text-medium-emphasis mt-1">{{ t('import.ignored', { item: i }) }}</div>

          <div class="d-flex justify-end mt-4">
            <v-btn color="primary" :loading="importing" :disabled="selected.size === 0" @click="confirmImport">
              {{ t('import.importSelected', { n: selected.size }) }}
            </v-btn>
          </div>
        </v-card>
        <v-card v-else class="pa-6">
          <div class="text-medium-emphasis">{{ t('import.previewEmpty') }}</div>
        </v-card>
      </v-col>
    </v-row>

    <v-snackbar v-model="snackbar" :color="snackbarColor" location="top right" :timeout="4000">
      {{ snackbarText }}
    </v-snackbar>
  </div>
</template>

<style scoped>
.font-mono :deep(textarea) {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.85rem;
}
</style>
