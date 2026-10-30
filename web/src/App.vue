<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import AppHeader from './components/AppHeader.vue'
import BottomNav from './components/BottomNav.vue'
import MoreSheet from './components/MoreSheet.vue'

const route = useRoute()
const isPublic = computed(() => route.meta.public === true)
</script>

<template>
  <div v-if="isPublic" class="auth-shell">
    <router-view />
  </div>
  <div v-else class="app-shell">
    <AppHeader />
    <BottomNav />
    <MoreSheet />
    <main class="app-main" role="main">
      <router-view />
    </main>
  </div>
</template>
