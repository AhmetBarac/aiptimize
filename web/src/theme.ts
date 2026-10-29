import { ref } from 'vue'

export type Theme = 'dark' | 'light'

const STORAGE_KEY = 'aiptimize_theme'

export const theme = ref<Theme>('dark')

function apply(): void {
  document.documentElement.dataset.bsTheme = theme.value
  const meta = document.querySelector('meta[name="theme-color"]')
  if (meta) meta.setAttribute('content', theme.value === 'dark' ? '#000000' : '#f2f2f7')
  localStorage.setItem(STORAGE_KEY, theme.value)
}

export function initTheme(): void {
  const stored = localStorage.getItem(STORAGE_KEY)
  theme.value = stored === 'light' ? 'light' : 'dark'
  apply()
}

export function setTheme(value: Theme): void {
  theme.value = value
  apply()
}

export function toggleTheme(): void {
  setTheme(theme.value === 'dark' ? 'light' : 'dark')
}
