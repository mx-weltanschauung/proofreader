import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Второй бэкенд из worktree (замер, приёмка) живёт на своём порту.
const apiTarget = process.env.API_TARGET ?? 'http://localhost:8080'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 3100,
    proxy: {
      '/api': {
        target: apiTarget,
        changeOrigin: true,
      },
      '/uploads': {
        target: apiTarget,
        changeOrigin: true,
      },
    },
  },
})
