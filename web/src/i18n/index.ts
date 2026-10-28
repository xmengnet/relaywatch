import { createI18n } from 'vue-i18n'
import zhHans from './zh-Hans'
import en from './en'

const saved = localStorage.getItem('rw-lang')

export const i18n = createI18n({
  legacy: false,
  locale: saved || (navigator.language.startsWith('en') ? 'en' : 'zh-Hans'),
  fallbackLocale: 'zh-Hans',
  messages: { 'zh-Hans': zhHans, en },
})

// <html lang> 与生效语言保持一致（后续切换由 useLocale 同步）
document.documentElement.lang = i18n.global.locale.value === 'en' ? 'en' : 'zh-Hans'
