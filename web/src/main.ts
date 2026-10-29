import { createApp } from 'vue'
import 'bootstrap/dist/css/bootstrap.min.css'
import 'bootstrap-icons/font/bootstrap-icons.css'
import './styles/main.css'
import App from './App.vue'
import { i18n } from './i18n'
import { router } from './router'
import { initTheme } from './theme'

initTheme()
document.documentElement.lang = i18n.global.locale.value

createApp(App).use(router).use(i18n).mount('#app')
