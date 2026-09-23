import { fileURLToPath } from 'node:url'
import { expect, test, type APIRequestContext } from '@playwright/test'

// A bundle the project wiki mounts, as a person meets it (docs/design.md
// 5.9): they name its folder in the project's settings; the Wiki tab then
// lists its pages in a group of its own, shows them read-only, and search
// finds them with the wiki's own. The bundle is OKF's sample, a retailer's
// data catalog.

const api = '/api/v1'
const acmeRetail = fileURLToPath(new URL('../../internal/wiki/okf/testdata/acme_retail', import.meta.url))

async function call<T>(ctx: APIRequestContext, method: 'get' | 'post', path: string, data?: unknown): Promise<T> {
  const res = method === 'get' ? await ctx.get(api + path) : await ctx.post(api + path, { data })
  expect(res.ok(), `${method.toUpperCase()} ${path} -> ${res.status()} ${await res.text()}`).toBeTruthy()
  return (await res.json()) as T
}

test('a bundle from elsewhere: mounted in the settings, read in the Wiki tab', async ({ page }) => {
  const request = page.request
  const stamp = Date.now().toString(36)
  const account = { name: 'e2e', password: 'e2e-password' }
  const status = await call<{ setup_required: boolean }>(request, 'get', '/auth/status')
  await call(request, 'post', status.setup_required ? '/auth/setup' : '/auth/login', account)
  const name = `catalog ${stamp}`
  const { rooms } = await call<{ rooms: { id: string }[] }>(request, 'post', '/projects', { name, repo_path: '', agent_ids: [] })
  const roomId = rooms[0].id

  // The folder, in the project's settings.
  await page.goto(`/rooms/${roomId}`)
  await page.getByRole('button', { name: '群聊信息' }).click()
  await page.getByRole('button', { name: `编辑项目 ${name}` }).click()
  const dialog = page.getByRole('dialog', { name: '编辑项目' })
  await dialog.getByLabel('外部 wiki（只读）').fill('relative/folder')
  await dialog.getByRole('button', { name: '保存' }).click()
  await expect(dialog).toContainText('relative/folder 不是完整路径')
  await expect(dialog).not.toContainText('store:')
  await dialog.getByLabel('外部 wiki（只读）').fill(acmeRetail)
  await dialog.getByRole('button', { name: '保存' }).click()
  await expect(dialog).toBeHidden()

  // Its pages, a group of their own in the Wiki tab.
  await page.goto(`/rooms/${roomId}/wiki`)
  const pages = page.getByLabel('页面')
  await expect(page.getByRole('region', { name: '外部 wiki' })).toContainText('acme-retail')
  await pages.getByRole('button', { name: /外部 · acme-retail/ }).click()
  await pages.getByRole('link', { name: 'Gross Margin', exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/rooms/${roomId}/wiki/@acme-retail/metrics/gross-margin\\.md$`))
  await expect(page.getByRole('heading', { name: 'Gross Margin', exact: true })).toBeVisible()
  await expect(page.getByText('外部 · acme-retail · 只读')).toBeVisible()
  await expect(page.getByRole('button', { name: '确认' })).toHaveCount(0)

  // Its links stay in it.
  await page.getByRole('article').getByRole('link', { name: 'Revenue', exact: true }).first().click()
  await expect(page).toHaveURL(new RegExp(`/wiki/@acme-retail/metrics/revenue\\.md$`))

  // Search finds its pages with the wiki's own, and says where they are.
  await page.getByRole('searchbox', { name: '搜索 wiki' }).fill('gross margin')
  const hit = pages.getByRole('link', { name: /Gross Margin.*外部 · acme-retail/ }).first()
  await expect(hit).toBeVisible()
})
