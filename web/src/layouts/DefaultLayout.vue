<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useDisplay } from 'vuetify'
import { useAuthStore } from '@/stores/auth'
import { useI18n } from 'vue-i18n'
import { useTheme } from '@/composables/useTheme'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { t } = useI18n()
const { mdAndUp } = useDisplay()
const { mode } = useTheme()

const drawer = ref(mdAndUp.value)

const navItems = computed(() => [
  { to: '/', icon: 'mdi-view-dashboard-outline', title: t('nav.dashboard') },
  { to: '/monitors', icon: 'mdi-radar', title: t('nav.monitors') },
  { to: '/import', icon: 'mdi-import', title: '导入' },
  { to: '/channels', icon: 'mdi-bell-ring-outline', title: t('nav.channels') },
  { to: '/notifications', icon: 'mdi-email-outline', title: t('nav.notifications') },
  { to: '/settings', icon: 'mdi-cog-outline', title: t('nav.settings') },
])

const themeIcon = computed(() => (mode.value === 'dark' ? 'mdi-weather-night' : 'mdi-white-balance-sunny'))

function toggleTheme() {
  mode.value = mode.value === 'dark' ? 'light' : 'dark'
}

async function doLogout() {
  await auth.logout()
  router.push('/login')
}
</script>

<template>
  <v-app>
    <v-navigation-drawer v-model="drawer" :permanent="mdAndUp" :temporary="!mdAndUp" width="240">
      <v-list nav density="comfortable">
        <v-list-item v-for="item in navItems" :key="item.to" :to="item.to" :prepend-icon="item.icon" :title="item.title" />
      </v-list>
      <template #append>
        <div class="pa-4 text-caption text-medium-emphasis">RelayWatch</div>
      </template>
    </v-navigation-drawer>

    <v-app-bar flat border="b">
      <v-app-bar-nav-icon v-if="!mdAndUp" @click="drawer = !drawer" aria-label="菜单" />
      <v-app-bar-title>RelayWatch</v-app-bar-title>
      <template #append>
        <v-btn :icon="themeIcon" variant="text" aria-label="切换主题" @click="toggleTheme" />
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
