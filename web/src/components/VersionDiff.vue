<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ oldVersion?: string | null; newVersion?: string | null }>()

const showBoth = computed(() => props.oldVersion && props.newVersion && props.oldVersion !== props.newVersion)
</script>

<template>
  <span class="version-diff">
    <span v-if="showBoth" class="old">{{ oldVersion }}</span>
    <v-icon v-if="showBoth" size="x-small">mdi-arrow-right</v-icon>
    <span :class="{ newer: showBoth }">{{ newVersion ?? '—' }}</span>
  </span>
</template>

<style scoped>
.version-diff { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: .875rem; }
.old { text-decoration: line-through; opacity: .5; }
.newer { color: rgb(var(--v-theme-warning)); font-weight: 600; }
</style>
