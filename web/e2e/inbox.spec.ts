import { expect, test, type APIRequestContext } from '@playwright/test'

// The inbox is live (docs/webui.md §5.2): with it open and nothing
// reloaded, a request an agent raises in some project shows up, goes once
// decided, and the answer addressed to the person takes its place. The
// fake runtime plays the agent.

const api = '/api/v1'

async function call<T>(ctx: APIRequestContext, method: 'get' | 'post', path: string, data?: unknown): Promise<T> {
  const res = method === 'get' ? await ctx.get(api + path) : await ctx.post(api + path, { data })
  expect(res.ok(), `${method.toUpperCase()} ${path} -> ${res.status()} ${await res.text()}`).toBeTruthy()
  return (await res.json()) as T
}

test('what reaches the person shows in the open inbox as it happens', async ({ page }) => {
  const request = page.request
  const stamp = Date.now().toString(36)
  const account = { name: 'e2e', password: 'e2e-password' }
  const status = await call<{ setup_required: boolean }>(request, 'get', '/auth/status')
  await call(request, 'post', status.setup_required ? '/auth/setup' : '/auth/login', account)
  await expect
    .poll(async () => (await call<{ machines: { id: string }[] }>(request, 'get', '/machines')).machines.length, { timeout: 30_000 })
    .toBeGreaterThan(0)
  const { machines } = await call<{ machines: { id: string }[] }>(request, 'get', '/machines')
  const careful = `Careful ${stamp}`
  const { agent } = await call<{ agent: { id: string } }>(request, 'post', '/agents', {
    name: careful,
    machine_id: machines[0].id,
    runtime: 'fake',
    role_card: 'Ask first.',
    permission_preset: 'edit_with_approval',
    runtime_options: { approval: true, reply: `构建好了 ${stamp}` },
  })
  const { rooms } = await call<{ rooms: { id: string }[] }>(request, 'post', '/projects', { name: `inbox ${stamp}`, repo_path: '', agent_ids: [agent.id] })
  const { members } = await call<{ members: { id: string }[] }>(request, 'get', `/rooms/${rooms[0].id}/members`)
  const { user } = await call<{ user: { id: string } }>(request, 'get', '/auth/status')

  await page.goto('/inbox')
  const forMe = page.getByRole('main')
  await expect(forMe.getByRole('heading', { name: '收件箱' })).toBeVisible()

  // Asked in its project, the agent asks permission: the request shows.
  await call(request, 'post', `/rooms/${rooms[0].id}/messages`, {
    user_id: user.id,
    body: `@${careful} 构建一下`,
    mentions: [{ kind: 'agent', id: members[0].id }],
  })
  const asked = forMe.getByRole('link').filter({ hasText: careful }).filter({ hasText: '待审批' })
  await expect(asked).toContainText('make test', { timeout: 20_000 })

  // Decided elsewhere, it goes; the answer addressed to the person comes.
  const { approvals } = await call<{ approvals: { id: string; member_name: string }[] }>(request, 'get', '/approvals?status=pending')
  const pending = approvals.find((a) => a.member_name === careful)
  expect(pending).toBeDefined()
  await call(request, 'post', `/approvals/${pending?.id}/decide`, { user_id: user.id, allow: true, message: '' })
  await expect(asked).toHaveCount(0)
  const answer = forMe.getByRole('link').filter({ hasText: `构建好了 ${stamp}` })
  await expect(answer).toContainText(careful)

  // Unread until opened, and counted meanwhile (docs/webui.md 4.19): the
  // signed-in account, which is no users row, reads it all the same. An
  // unread row is lifted onto the page's background.
  await expect(answer).toHaveAttribute('data-unread')
  const unread = async () => (await call<{ unread: number }>(request, 'get', `/users/${user.id}/inbox`)).unread
  const before = await unread()
  await answer.click()
  await expect(answer).not.toHaveAttribute('data-unread')
  await expect.poll(unread).toBe(before - 1)
})
