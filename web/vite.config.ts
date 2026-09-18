import { fileURLToPath, URL } from 'node:url'
import { loadEnv } from 'vite'
import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// Settings live in the repo root's .env, copied from .env.example, the
// same file `veyloom serve` reads: VEYLOOM_API says where the dev server
// and `vite preview` proxy /api (HTTP and WebSocket), VITE_API_BASE makes
// the client call another origin instead (see src/api/base.ts). A real
// environment variable wins over the file.
const root = fileURLToPath(new URL('..', import.meta.url))

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, root, '')
  const proxy = {
    '/api': { target: env.VEYLOOM_API || 'http://127.0.0.1:7788', ws: true },
  }
  return {
    plugins: [react(), tailwindcss()],
    envDir: root,
    // Each dev server instance keeps its own pre-bundle cache. A second
    // instance (the 7791 preview, an e2e run) writing into the same
    // node_modules/.vite pulls the chunks out from under the one on 7789,
    // whose open pages then fail to import them. Any instance that is not
    // the default one sets VEYLOOM_VITE_CACHE.
    cacheDir: env.VEYLOOM_VITE_CACHE || 'node_modules/.vite',
    resolve: {
      alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
    },
    // Ports next to the API's 7788, and fixed: a taken port fails loudly
    // instead of quietly landing somewhere else.
    server: {
      port: 7789,
      strictPort: true,
      proxy,
    },
    preview: {
      port: 7790,
      strictPort: true,
      proxy,
    },
    test: {
      environment: 'jsdom',
      setupFiles: ['./src/test/setup.ts'],
      css: false,
      // Playwright owns e2e/.
      exclude: ['e2e/**', 'node_modules/**'],
    },
  }
})
