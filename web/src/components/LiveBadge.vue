<script setup lang="ts">
withDefaults(
  defineProps<{
    tone: 'live' | 'stale' | 'waiting' | 'offline'
    label: string
    detail?: string
  }>(),
  { detail: '' },
)
</script>

<template>
  <span class="live-badge" :class="`live-${tone}`" role="status">
    <span class="live-dot" aria-hidden="true"></span>
    <span>{{ label }}</span>
    <span v-if="detail" class="live-detail">{{ detail }}</span>
  </span>
</template>
