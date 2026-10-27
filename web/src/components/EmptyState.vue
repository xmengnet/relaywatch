<script setup lang="ts">
defineProps<{ to?: string }>()
</script>

<template>
  <v-empty-state
    icon="mdi-radar"
    headline="还没有监控"
    text="添加第一个监控，自动跟踪上游版本更新"
  >
    <template #actions>
      <v-btn v-if="to" :to="to" color="primary" prepend-icon="mdi-plus">新建监控</v-btn>
    </template>
  </v-empty-state>
</template>
