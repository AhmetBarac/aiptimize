<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import PageHead from '../components/PageHead.vue'
import PhaseNotice from '../components/PhaseNotice.vue'

const { t } = useI18n()
</script>

<template>
  <PageHead :title="t('plan.title')" :subtitle="t('plan.subtitle')" />
  <PhaseNotice
    icon="bi-calendar3-week"
    :title="t('plan.title')"
    :body="t('plan.comingSoon')"
    :phase="`${t('common.phase')} 4–5`"
  />
</template>
