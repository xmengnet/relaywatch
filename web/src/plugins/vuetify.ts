import 'vuetify/styles'
import '@mdi/font/css/materialdesignicons.css'
import { createVuetify } from 'vuetify'
import { aliases, mdi } from 'vuetify/iconsets/mdi'

export const vuetify = createVuetify({
  icons: { defaultSet: 'mdi', aliases, sets: { mdi } },
  theme: {
    defaultTheme: 'light',
    themes: {
      light: {
        dark: false,
        colors: {
          primary: '#2563eb',
          secondary: '#64748b',
          success: '#16a34a',
          warning: '#d97706',
          error: '#dc2626',
          info: '#0284c7',
          surface: '#ffffff',
          background: '#f6f7f9',
        },
      },
      dark: {
        dark: true,
        colors: {
          primary: '#3b82f6',
          secondary: '#94a3b8',
          success: '#22c55e',
          warning: '#f59e0b',
          error: '#ef4444',
          info: '#38bdf8',
          surface: '#1e2128',
          background: '#16181d',
        },
      },
    },
  },
  defaults: {
    VBtn: { variant: 'flat', rounded: 'lg' },
    VTextField: { variant: 'outlined', density: 'comfortable' },
    VSelect: { variant: 'outlined', density: 'comfortable' },
    VTextarea: { variant: 'outlined', density: 'comfortable' },
    VDataTable: { density: 'comfortable' },
    VCard: { rounded: 'lg', elevation: 0, border: true },
    VDialog: { maxWidth: 640 },
    VSnackbar: { location: 'top right', timeout: 4000 },
  },
})
