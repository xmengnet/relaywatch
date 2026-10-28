import { useI18n } from 'vue-i18n'
import { vuetify } from '@/plugins/vuetify'

export type Locale = 'zh-Hans' | 'en'

export const localeOptions: Array<{ value: Locale; title: string }> = [
  { value: 'zh-Hans', title: '简体中文' },
  { value: 'en', title: 'English' },
]

// 界面语言：读写 vue-i18n 全局 locale，持久化到 localStorage（与 useTheme 的 rw-theme 同风格）
export function useLocale() {
  const { locale } = useI18n({ useScope: 'global' })

  const setLocale = (raw: unknown) => {
    const next: Locale = raw === 'en' ? 'en' : 'zh-Hans'
    locale.value = next
    vuetify.locale.current.value = next === 'en' ? 'en' : 'zhHans'
    localStorage.setItem('rw-lang', next)
    document.documentElement.lang = next === 'en' ? 'en' : 'zh-Hans'
  }

  return { locale, setLocale, localeOptions }
}