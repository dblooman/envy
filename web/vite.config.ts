import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/v1': {
        target: 'http://127.0.0.1:8081',
        changeOrigin: true,
      },
      '/healthz': {
        target: 'http://127.0.0.1:8081',
        changeOrigin: true,
      },
      '/readyz': {
        target: 'http://127.0.0.1:8081',
        changeOrigin: true,
      },
    },
  },
})
