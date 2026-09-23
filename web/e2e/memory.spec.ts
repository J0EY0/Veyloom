import { expect, test, type APIRequestContext } from '@playwright/test'

// The memories, as a person meets them (docs/design.md 5.16, 5.19): a
// member notes what it is asked to remember in the project memory; the
// person adds to it in the Wiki tab, and to the global memory in Settings,
// and undoes a change there. The fake runtime plays the member.

const api = '/api/v1'

async function call<T>(ctx: APIRequestContext, method: 'get' | 'post', path: string, data?: unknown): Promise<T> {
  const res = method === 'get' ? await ctx.get(api + path) : await ctx.post(api + path, { data })
  expect(res.ok(), `${method.toUpperCase()} ${path} -> ${res.status()} ${await res.text()}`).toBeTruthy()
  return (await res.json()) as T
}

interface Memory {
  entries: { text: string; date?: string; source?: string }[]
}

test('a member remembers, and a person keeps both memories', async ({ page }) => {
  const request = page.request
  const stamp = Date.now().toString(36)
  const account = { name: 'e2e', password: 'e2e-password' }
  const status = await call<{ setup_required: boolean }>(request, 'get', '/auth/status')
  await call(request, 'post', status.setup_required ? '/auth/setup' : '/auth/login', account)
  await expect
    .poll(async () => (await call<{ machines: { id: string }[] }>(request, 'get', '/machines')).machines.length, { timeout: 30_000 })
    .toBeGreaterThan(0)
  const { machines } = await call<{ machines: { id: string }[] }>(request, 'get', '/machines')

  const rule = `改完先跑 make test-db 再说做完 ${stamp}`
  const { agent } = await call<{ agent: { id: string } }>(request, 'post', '/agents', {
    name: `Noter ${stamp}`,
    machine_id: machines[0].id,
    runtime: 'fake',
    role_card: 'Note what you are told to.',
    permission_preset: 'read_only',
    runtime_options: { reply: '记下了。', tool_calls: [{ tool: 'remember', args: { entries: [rule] } }] },
  })
  const { project, rooms } = await call<{ project: { id: string }; rooms: { id: string }[] }>(request, 'post', '/projects', {
    name: `memory ${stamp}`,
    repo_path: '',
    agent_ids: [agent.id],
  })
  const roomId = rooms[0].id
  // The one member of the chat hears a message without an @.
  await call(request, 'post', `/rooms/${roomId}/messages`, { body: '记住：改完先跑 make test-db 再说做完' })
  await expect
    .poll(async () => (await call<{ memory: Memory }>(request, 'get', `/projects/${project.id}/memory`)).memory.entries.map((e) => e.text))
    .toEqual([rule])

  // The Wiki tab: the project memory has a place of its own, with the
  // member's entry and where it came from.
  await page.goto(`/rooms/${roomId}/wiki`)
  await page.getByLabel('页面').getByRole('link', { name: '项目记忆' }).click()
  await expect(page).toHaveURL(new RegExp(`/rooms/${roomId}/wiki/memory$`))
  await expect(page.getByRole('heading', { name: '项目记忆' })).toBeVisible()
  const entries = page.getByRole('list', { name: '条目' })
  await expect(entries.getByText(rule)).toBeVisible()
  await expect(entries.getByText(new RegExp(`Noter ${stamp} · 话题 #1$`))).toBeVisible()

  // The person adds one; it is theirs, and still there after a reload.
  const mine = `回复用中文 ${stamp}`
  await page.getByRole('textbox', { name: '新的一条' }).fill(mine)
  await page.getByRole('button', { name: '添加' }).click()
  await expect(entries.getByText(mine)).toBeVisible()
  await page.reload()
  await expect(entries.getByText(mine)).toBeVisible()
  await expect(entries.getByText(/ · e2e$/)).toBeVisible()

  // Settings, General: the global memory, every project's, kept in a
  // dialog (docs/design.md 5.19). What is added there is undone again, so
  // runs on the same database leave it as they found it.
  await page.goto('/settings/general')
  await page.getByRole('region', { name: '记忆' }).getByRole('button', { name: '管理' }).click()
  const memory = page.getByRole('dialog', { name: '全局记忆' })
  const everywhere = `提交说明用英文 ${stamp}`
  await memory.getByRole('textbox', { name: '新的一条' }).fill(everywhere)
  await memory.getByRole('button', { name: '添加' }).click()
  await expect(memory.getByRole('list', { name: '条目' }).getByText(everywhere)).toBeVisible()
  await memory.getByRole('button', { name: '更多' }).click()
  await page.getByRole('menuitem', { name: '改动记录' }).click()
  await memory.getByRole('button', { name: '撤回' }).first().click()
  const confirm = page.getByRole('alertdialog', { name: '撤回这次改动？' })
  await confirm.getByRole('button', { name: '撤回' }).click()
  await memory.getByRole('button', { name: '更多' }).click()
  await page.getByRole('menuitem', { name: '条目' }).click()
  await expect(memory.getByRole('list', { name: '条目' }).getByText(everywhere)).toHaveCount(0)
})
