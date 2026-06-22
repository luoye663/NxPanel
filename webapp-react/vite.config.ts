import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8888',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: '../web/dist',
    emptyOutDir: true,
    chunkSizeWarningLimit: 1000,
  },
})
