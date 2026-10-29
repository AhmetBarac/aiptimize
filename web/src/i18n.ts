import { createI18n } from 'vue-i18n'
import en from './locales/en.json'
import sv from './locales/sv.json'

export const SUPPORTED_LOCALES = ['en', 'sv'] as const
export type Locale = (typeof SUPPORTED_LOCALES)[number]

function initialLocale(): Locale {
  const stored = localStorage.getItem('aiptimize_locale')
  if (stored === 'en' || stored === 'sv') return stored
  if (navigator.language?.toLowerCase().startsWith('sv')) return 'sv'
  return 'en'
}

export const i18n = createI18n({
  legacy: false,
  locale: initialLocale(),
  fallbackLocale: 'en',
  messages: { en, sv },
})

export function setLocale(locale: Locale): void {
  i18n.global.locale.value = locale
  localStorage.setItem('aiptimize_locale', locale)
  document.documentElement.lang = locale
}

export function currentLocale(): Locale {
  return i18n.global.locale.value === 'sv' ? 'sv' : 'en'
}
