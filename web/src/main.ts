import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { vuetify } from './plugins/vuetify'
import { router } from './router'
import { i18n } from './i18n'
import 'vuetify/styles'
import './styles/global.css'
import '@mdi/font/css/materialdesignicons.css'

createApp(App).use(createPinia()).use(router).use(i18n).use(vuetify).mount('#app')
