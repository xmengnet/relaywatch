import { ref, watch } from 'vue'
import { useTheme as useVuetifyTheme } from 'vuetify'

export type ThemeMode = 'light' | 'dark' | 'auto'

const mode = ref<ThemeMode>((localStorage.getItem('rw-theme') as ThemeMode) || 'auto')

export function useTheme() {
  const theme = useVuetifyTheme()

  const apply = () => {
    if (mode.value === 'auto') {
      const dark = window.matchMedia('(prefers-color-scheme: dark)').matches
      theme.global.name.value = dark ? 'dark' : 'light'
    } else {
      theme.global.name.value = mode.value
    }
    localStorage.setItem('rw-theme', mode.value)
  }

  apply()
  watch(mode, apply)

  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
    if (mode.value === 'auto') apply()
  })

  return { mode, apply }
}
