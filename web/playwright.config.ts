import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { defineConfig } from '@playwright/test'

// End-to-end tests run the real pieces the way they are deployed: the API
// (`veyloom serve` with the fake runtime) on the veyloom_test database and
// a temp state dir, and the built web client from `vite preview`, which
// proxies /api to it. `make web-e2e` builds both first; Playwright starts
// and stops the two servers.
const apiPort = 7797
const webPort = 4197
const databaseUrl = process.env.E2E_DATABASE_URL ?? 'postgres://veyloom:veyloom@localhost:5432/veyloom_test?sslmode=disable'

export default defineConfig({
  testDir: './e2e',
  timeout: 60_000,
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: 'list',
  use: {
    baseURL: `http://127.0.0.1:${webPort}`,
    locale: 'zh-CN',
    trace: 'retain-on-failure',
  },
  webServer: [
    {
      // --env-file= keeps the test server off any .env around and stops it
      // writing one.
      command: `../bin/veyloom serve --env-file= --addr 127.0.0.1:${apiPort} --state-dir ${join(tmpdir(), 'veyloom-e2e')} --database-url "${databaseUrl}"`,
      url: `http://127.0.0.1:${apiPort}/api/v1/projects`,
      timeout: 30_000,
      reuseExistingServer: false,
    },
    {
      command: `npx vite preview --host 127.0.0.1 --port ${webPort} --strictPort`,
      url: `http://127.0.0.1:${webPort}/`,
      timeout: 30_000,
      reuseExistingServer: false,
      env: { VEYLOOM_API: `http://127.0.0.1:${apiPort}` },
    },
  ],
})
