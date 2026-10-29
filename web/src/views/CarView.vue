<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import PageHead from '../components/PageHead.vue'
import PhaseNotice from '../components/PhaseNotice.vue'

const { t } = useI18n()
</script>

<template>
  <PageHead :title="t('car.title')" :subtitle="t('car.subtitle')" />
  <PhaseNotice
    icon="bi-ev-station"
    :title="t('car.title')"
    :body="t('car.comingSoon')"
    :phase="`${t('common.phase')} 6`"
  />
</template>
