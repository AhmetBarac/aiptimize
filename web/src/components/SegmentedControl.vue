<script setup lang="ts">
interface SegmentOption {
  value: string
  label: string
  icon?: string
}

withDefaults(
  defineProps<{
    modelValue: string
    options: SegmentOption[]
    groupLabel: string
    size?: 'sm' | 'md'
  }>(),
  { size: 'md' },
)

const emit = defineEmits<{ (event: 'update:modelValue', value: string): void }>()
</script>

<template>
  <div class="segmented" :class="`segmented-${size}`" role="group" :aria-label="groupLabel">
    <button
      v-for="option in options"
      :key="option.value"
      class="segmented-item"
      :class="{ active: modelValue === option.value }"
      type="button"
      :aria-pressed="modelValue === option.value"
      @click="emit('update:modelValue', option.value)"
    >
      <i v-if="option.icon" class="bi" :class="option.icon" aria-hidden="true"></i>
      <span>{{ option.label }}</span>
    </button>
  </div>
</template>
