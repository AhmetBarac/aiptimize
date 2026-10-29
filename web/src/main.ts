import { createApp } from 'vue'
import 'bootstrap/dist/css/bootstrap.min.css'
import 'bootstrap-icons/font/bootstrap-icons.css'
import './styles/main.css'
import App from './App.vue'
import { i18n } from './i18n'
import { router } from './router'

function applyTheme(): void {
  const dark = window.matchMedia('(prefers-color-scheme: dark)').matches
  document.documentElement.dataset.bsTheme = dark ? 'dark' : 'light'
  const meta = document.querySelector('meta[name="theme-color"]')
  if (meta) meta.setAttribute('content', dark ? '#0a1120' : '#f4f6fb')
}

applyTheme()
window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', applyTheme)
document.documentElement.lang = i18n.global.locale.value

createApp(App).use(router).use(i18n).mount('#app')
