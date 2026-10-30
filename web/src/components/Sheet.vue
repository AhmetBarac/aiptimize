<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const props = withDefaults(
  defineProps<{
    open: boolean
    title: string
    subtitle?: string
    size?: 'sm' | 'md'
  }>(),
  { subtitle: '', size: 'md' },
)

const emit = defineEmits<{ (event: 'update:open', value: boolean): void }>()

const panel = ref<HTMLElement | null>(null)
const titleId = useId()
let previousFocus: HTMLElement | null = null
let previousOverflow = ''

function close(): void {
  emit('update:open', false)
}

function focusable(): HTMLElement[] {
  if (!panel.value) return []
  const nodes = panel.value.querySelectorAll<HTMLElement>(
    'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
  )
  return [...nodes].filter((node) => node.offsetParent !== null)
}

function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    event.preventDefault()
    close()
    return
  }
  if (event.key !== 'Tab') return
  const nodes = focusable()
  if (nodes.length === 0) {
    event.preventDefault()
    panel.value?.focus()
    return
  }
  const first = nodes[0]
  const last = nodes[nodes.length - 1]
  if (event.shiftKey && (document.activeElement === first || document.activeElement === panel.value)) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault()
    first.focus()
  }
}

function lock(): void {
  previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
  previousOverflow = document.body.style.overflow
  document.body.style.overflow = 'hidden'
  document.addEventListener('keydown', onKeydown)
}

function unlock(): void {
  document.removeEventListener('keydown', onKeydown)
  document.body.style.overflow = previousOverflow
  previousFocus?.focus()
  previousFocus = null
}

watch(
  () => props.open,
  async (open) => {
    if (open) {
      lock()
      await nextTick()
      const nodes = focusable()
      ;(nodes[0] ?? panel.value)?.focus()
    } else {
      unlock()
    }
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  if (props.open) unlock()
})
</script>

<template>
  <Teleport to="body">
    <Transition name="sheet">
      <div v-if="open" class="sheet-root">
        <button
          class="sheet-backdrop"
          type="button"
          :aria-label="t('common.close')"
          tabindex="-1"
          @click="close"
        ></button>
        <section
          ref="panel"
          class="sheet-panel"
          :class="`sheet-panel-${size}`"
          role="dialog"
          aria-modal="true"
          :aria-labelledby="titleId"
          tabindex="-1"
        >
          <header class="sheet-head">
            <div class="sheet-title-wrap">
              <h2 :id="titleId" class="sheet-title">{{ title }}</h2>
              <p v-if="subtitle" class="sheet-subtitle">{{ subtitle }}</p>
            </div>
            <button class="icon-button sheet-close" type="button" :aria-label="t('common.close')" @click="close">
              <i class="bi bi-x-lg" aria-hidden="true"></i>
            </button>
          </header>
          <div class="sheet-body">
            <slot />
          </div>
          <footer v-if="$slots.footer" class="sheet-footer">
            <slot name="footer" />
          </footer>
        </section>
      </div>
    </Transition>
  </Teleport>
</template>
