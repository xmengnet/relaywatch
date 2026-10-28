import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

type Localizable = { label: string; label_en?: string }

// 后端注册表（监控源/渠道）同时返回中英文文案，这里按当前界面语言选择。
export function useMetaText() {
  const { locale } = useI18n()
  const isEn = computed(() => locale.value === 'en')

  function pick(zh?: string, en?: string): string {
    return (isEn.value ? en : zh) || zh || en || ''
  }

  return {
    isEn,
    label: (m: Localizable) => pick(m.label, m.label_en),
    help: (m: { help?: string; help_en?: string }) => pick(m.help, m.help_en),
    placeholder: (m: { placeholder?: string; placeholder_en?: string }) => pick(m.placeholder, m.placeholder_en),
  }
}
