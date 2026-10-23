import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'node:path'

// Geliştirme: `joe serve --no-browser` + `pnpm dev` (API istekleri 8777'ye yönlenir).
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { '@': path.resolve(import.meta.dirname, 'src') } },
  server: {
    port: 5173,
    proxy: { '/api': { target: 'http://127.0.0.1:8777', changeOrigin: false } },
  },
  build: { outDir: 'dist', chunkSizeWarningLimit: 1200 },
  test: { environment: 'node' },
})
