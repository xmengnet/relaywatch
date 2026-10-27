<script setup lang="ts">
import { useTheme } from '@/composables/useTheme'

useTheme()
</script>

<template>
  <v-app>
    <v-main class="d-flex align-center justify-center" style="min-height: 100vh">
      <router-view />
    </v-main>
  </v-app>
</template>
