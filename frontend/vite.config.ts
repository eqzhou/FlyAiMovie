import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  server: {
    port: 3013,
    proxy: {
      '/api': { target: 'http://localhost:5679', changeOrigin: true },
      '/static': { target: 'http://localhost:5679', changeOrigin: true },
    },
  },
})
