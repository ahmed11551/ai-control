import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    host: '127.0.0.1',
    port: 5173,
    proxy: {
      '/api': {
        target: process.env.API_PROXY || 'http://127.0.0.1:8080',
        rewrite: (path) => path.replace(/^\/api/, ''),
      },
      '/ws': {
        // http target + ws:true — надёжнее, чем ws:// (иначе иногда 000 на upgrade)
        target: process.env.API_PROXY || 'http://127.0.0.1:8080',
        ws: true,
        changeOrigin: true,
      },
      '/webhooks': {
        target: process.env.API_PROXY || 'http://127.0.0.1:8080',
      },
    },
  },
})
