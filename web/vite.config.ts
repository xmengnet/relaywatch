import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import vuetify from 'vite-plugin-vuetify'

export default defineConfig({
  plugins: [vue(), vuetify({ autoImport: true })],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        manualChunks(id: string) {
          if (id.includes('node_modules/vuetify') || id.includes('node_modules/.vite/deps/vuetify')) return 'vuetify'
          return undefined
        },
      },
    },
  },
  server: {
    // Vite 8 默认只绑 IPv6 loopback，显式绑全回环避免 127.0.0.1 打不开
    host: '127.0.0.1',
    port: 5173,
    proxy: {
      // 后端地址跟 config.yaml 的 server.addr；默认 :8080
      '/api': process.env.RW_BACKEND ?? 'http://localhost:8080',
      '/auth': process.env.RW_BACKEND ?? 'http://localhost:8080',
    },
  },
})
