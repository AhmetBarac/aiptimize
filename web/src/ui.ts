import { reactive } from 'vue'

export const ui = reactive({
  moreOpen: false,
})

export function openMore(): void {
  ui.moreOpen = true
}

export function closeMore(): void {
  ui.moreOpen = false
}
