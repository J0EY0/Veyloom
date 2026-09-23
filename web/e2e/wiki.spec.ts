import { expect, test, type APIRequestContext } from '@playwright/test'

// The project wiki, as a person meets it (docs/design.md 5.15): what the
// agents write lands at once; a person confirms a page, and undoes a
// change they do not want, saying why. The fake runtime plays the agents.

const api = '/api/v1'

async function call<T>(ctx: APIRequestContext, method: 'get' | 'post', path: string, data?: unknown): Promise<T> {
  const res = method === 'get' ? await ctx.get(api + path) : await ctx.post(api + path, { data })
  expect(res.ok(), `${method.toUpperCase()} ${path} -> ${res.status()} ${await res.text()}`).toBeTruthy()
  return (await res.json()) as T
}

test('what agents write in the wiki, and what a person does with it', async ({ page }) => {
  const request = page.request
  const stamp = Date.now().toString(36)
  const account = { name: 'e2e', password: 'e2e-password' }
  const status = await call<{ setup_required: boolean }>(request, 'get', '/auth/status')
  await call(request, 'post', status.setup_required ? '/auth/setup' : '/auth/login', account)
  await expect
    .poll(async () => (await call<{ machines: { id: string }[] }>(request, 'get', '/machines')).machines.length, { timeout: 30_000 })
    .toBeGreaterThan(0)
  const { machines } = await call<{ machines: { id: string }[] }>(request, 'get', '/machines')

  const fact = `Hub 默认监听 7788 ${stamp}`
  const decision = `审批的 payload 用 json ${stamp}`
  const agent = async (who: string, args: Record<string, unknown>) =>
    (
      await call<{ agent: { id: string } }>(request, 'post', '/agents', {
        name: `${who} ${stamp}`,
        machine_id: machines[0].id,
        runtime: 'fake',
        role_card: 'Write things down.',
        permission_preset: 'read_only',
        runtime_options: { reply: '记好了。', tool_calls: [{ tool: 'write_wiki', args }] },
      })
    ).agent
  const scribe = await agent('Scribe', {
    type: 'Fact',
    slug: `hub-port-${stamp}`,
    title: fact,
    description: '不传 --addr 时的端口。',
    body: '默认 `127.0.0.1:7788`。',
    topics: [1],
  })
  const decider = await agent('Decider', {
    type: 'Decision',
    slug: `payload-json-${stamp}`,
    title: decision,
    description: 'jsonb 会重排键。',
    body: '用 json 原样保存。',
  })
  const { project, rooms } = await call<{ project: { id: string }; rooms: { id: string }[] }>(request, 'post', '/projects', {
    name: `wiki ${stamp}`,
    repo_path: '',
    agent_ids: [scribe.id, decider.id],
  })
  const roomId = rooms[0].id
  const { members } = await call<{ members: { id: string; display_name: string }[] }>(request, 'get', `/rooms/${roomId}/members`)
  const member = (who: string) => members.find((m) => m.display_name.startsWith(who))?.id ?? ''
  const written = async (title: string) =>
    (await call<{ wiki: { pages: { title: string }[] } }>(request, 'get', `/projects/${project.id}/wiki`)).wiki.pages.some((p) => p.title === title)
  await call(request, 'post', `/rooms/${roomId}/messages`, { body: '@Scribe 记一下端口', mentions: [{ kind: 'agent', id: member('Scribe') }] })
  await expect.poll(() => written(fact)).toBe(true)
  await call(request, 'post', `/rooms/${roomId}/messages`, { body: '@Decider 记下这个决定', mentions: [{ kind: 'agent', id: member('Decider') }] })
  await expect.poll(() => written(decision)).toBe(true)

  // The Wiki tab: both pages, nothing waiting.
  await page.goto(`/rooms/${roomId}`)
  const tabs = page.getByRole('navigation', { name: '群聊视图' })
  await tabs.getByRole('link', { name: 'Wiki' }).click()
  await expect(page).toHaveURL(new RegExp(`/rooms/${roomId}/wiki$`))
  const pages = page.getByLabel('页面')
  await expect(pages.getByRole('link', { name: decision })).toBeVisible()
  await pages.getByRole('link', { name: fact }).click()
  await expect(page.getByRole('heading', { name: fact })).toBeVisible()
  await expect(page.getByText('未核验')).toBeVisible()
  await expect(page.getByRole('button', { name: '话题 #1 里的一轮' })).toBeVisible()

  // A person confirms the fact.
  await page.getByRole('button', { name: '确认' }).click()
  await expect(page.getByText('人工审过')).toBeVisible()
  await expect(page.getByText(/e2e 确认于/)).toBeVisible()

  // And undoes the decision, saying why; the log keeps the reason.
  await pages.getByRole('link', { name: /变更/ }).click()
  const undo = page.getByRole('button', { name: '撤回' })
  const row = page.getByRole('listitem').filter({ hasText: decision }).filter({ has: undo })
  await row.getByRole('button', { name: '撤回' }).click()
  const dialog = page.getByRole('alertdialog', { name: '撤回这次改动？' })
  await dialog.getByLabel(/为什么撤回/).fill('还没定下来')
  await dialog.getByRole('button', { name: '撤回' }).click()
  await expect(pages.getByRole('link', { name: decision })).toHaveCount(0)
  await expect(page.getByText(/还没定下来/).first()).toBeVisible()

  // A doubt about the fact goes to whoever worked last, in the wiki topic,
  // its box already naming them and the page.
  await pages.getByRole('link', { name: fact }).click()
  await page.getByRole('button', { name: '有疑问…' }).click()
  await expect(page.getByPlaceholder('回复…')).toHaveValue(new RegExp(`^@Decider ${stamp} 对 wiki 页「${fact}」`))
})
