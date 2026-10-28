<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useDisplay } from 'vuetify'
import { useAuthStore } from '@/stores/auth'
import { useI18n } from 'vue-i18n'
import { useTheme } from '@/composables/useTheme'
import logoUrl from '@/assets/logo.svg'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { t } = useI18n()
const { mdAndUp } = useDisplay()
const { mode } = useTheme()

const drawer = ref(mdAndUp.value)

const navItems = computed(() => {
  const items = [
    { to: '/', icon: 'mdi-view-dashboard-outline', title: t('nav.dashboard') },
    { to: '/monitors', icon: 'mdi-radar', title: t('nav.monitors') },
  ]
  if (auth.can('import')) {
    items.push({ to: '/import', icon: 'mdi-import', title: t('nav.import') })
  }
  items.push({ to: '/channels', icon: 'mdi-bell-ring-outline', title: t('nav.channels') })
  items.push({ to: '/notifications', icon: 'mdi-email-outline', title: t('nav.notifications') })
  items.push({ to: '/help', icon: 'mdi-help-circle-outline', title: t('nav.help') })
  items.push({ to: '/profile', icon: 'mdi-account-circle-outline', title: t('nav.profile') })
  if (auth.user?.role === 'admin') {
    items.push({ to: '/site', icon: 'mdi-web', title: t('nav.site') })
    items.push({ to: '/users', icon: 'mdi-account-group-outline', title: t('nav.users') })
    items.push({ to: '/settings', icon: 'mdi-cog-outline', title: t('nav.settings') })
  }
  return items
})

const themeStates = [
  { value: 'light', icon: 'mdi-white-balance-sunny', key: 'themeLight' },
  { value: 'dark', icon: 'mdi-weather-night', key: 'themeDark' },
  { value: 'auto', icon: 'mdi-theme-light-dark', key: 'themeAuto' },
] as const

const themeState = computed(() => themeStates.find((s) => s.value === mode.value) ?? themeStates[0])
const themeIcon = computed(() => themeState.value.icon)
const themeLabel = computed(() => t(`common.${themeState.value.key}`))

function toggleTheme() {
  const idx = themeStates.findIndex((s) => s.value === mode.value)
  mode.value = themeStates[(idx + 1) % themeStates.length].value
}

async function doLogout() {
  await auth.logout()
  router.push('/login')
}
</script>

<template>
  <v-app>
    <v-navigation-drawer v-model="drawer" :permanent="mdAndUp" :temporary="!mdAndUp" width="240">
      <template #prepend>
        <div class="d-flex align-center pa-4 pb-2">
          <img :src="logoUrl" alt="RelayWatch logo" width="28" height="28" class="rounded me-2" />
          <span class="text-subtitle-1 font-weight-bold">RelayWatch</span>
        </div>
      </template>
      <v-list nav density="comfortable">
        <v-list-item v-for="item in navItems" :key="item.to" :to="item.to" :prepend-icon="item.icon" :title="item.title" />
      </v-list>
    </v-navigation-drawer>

    <v-app-bar flat border="b">
      <v-app-bar-nav-icon v-if="!mdAndUp" @click="drawer = !drawer" :aria-label="t('common.menu')" />
      <img :src="logoUrl" alt="RelayWatch logo" width="26" height="26" class="rounded ms-4 me-2" />
      <v-app-bar-title>RelayWatch</v-app-bar-title>
      <template #append>
        <v-btn icon="mdi-help-circle-outline" variant="text" to="/help" :aria-label="t('nav.help')" :title="t('nav.help')" />
        <v-btn :icon="themeIcon" variant="text" :aria-label="`${t('common.theme')}: ${themeLabel}`" :title="`${t('common.theme')}: ${themeLabel}`" @click="toggleTheme" />
        <v-btn class="me-2" variant="text" @click="doLogout">
          {{ auth.user?.username }} · {{ t('auth.logout') }}
        </v-btn>
      </template>
    </v-app-bar>

    <v-main>
      <div class="content">
        <router-view />
      </div>
    </v-main>
  </v-app>
</template>

<style scoped>
.content {
  max-width: 1400px;
  margin: 0 auto;
  padding: 24px;
}
</style>
