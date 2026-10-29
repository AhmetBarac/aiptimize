<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import PageHead from '../components/PageHead.vue'
import PhaseNotice from '../components/PhaseNotice.vue'

const { t } = useI18n()
</script>

<template>
  <PageHead :title="t('prices.title')" :subtitle="t('prices.subtitle')" />
  <PhaseNotice
    icon="bi-graph-up"
    :title="t('prices.title')"
    :body="t('prices.comingSoon')"
    :phase="`${t('common.phase')} 1`"
  />
</template>
