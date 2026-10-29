<script setup lang="ts">
defineProps<{
  icon: string
  title: string
  body: string
  phase: string
}>()
</script>

<template>
  <section class="aipt-card phase-notice">
    <div class="phase-icon" aria-hidden="true"><i class="bi" :class="icon"></i></div>
    <h2>{{ title }}</h2>
    <p>{{ body }}</p>
    <span class="phase-badge">{{ phase }}</span>
  </section>
</template>
