import { useI18n } from 'vue-i18n'

// 按界面语言格式化日期时间；浏览器语言与应用语言不一致时也能保持一致。
export function useFormat() {
  const { locale } = useI18n()

  function formatDateTime(value?: string | null): string {
    if (!value) return ''
    return new Date(value).toLocaleString(locale.value === 'en' ? 'en-US' : 'zh-CN')
  }

  return { formatDateTime }
}
