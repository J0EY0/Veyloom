import { expect, test, type APIRequestContext } from '@playwright/test'

// A person's message that names no member, as a person meets it
// (docs/design.md 4.2): the box says the leader takes it, to take on or
// hand on; sent, it goes to the leader, who hands it on to the member it
// fits, and that member works in the same topic. There, the box says the
// person is talking with the leader; with the leader switched off, it goes
// to whoever leads then. The fake runtime plays the members.

const api = '/api/v1'

async function call<T>(ctx: APIRequestContext, method: 'get' | 'post' | 'patch', path: string, data?: unknown): Promise<T> {
  const res = method === 'get' ? await ctx.get(api + path) : method === 'post' ? await ctx.post(api + path, { data }) : await ctx.patch(api + path, { data })
  expect(res.ok(), `${method.toUpperCase()} ${path} -> ${res.status()} ${await res.text()}`).toBeTruthy()
  return (await res.json()) as T
}

test('a message that names no member goes to the leader, who hands it on', async ({ page }) => {
  const request = page.request
  const stamp = Date.now().toString(36)
  const account = { name: 'e2e', password: 'e2e-password' }
  const status = await call<{ setup_required: boolean }>(request, 'get', '/auth/status')
  await call(request, 'post', status.setup_required ? '/auth/setup' : '/auth/login', account)
  await expect
    .poll(async () => (await call<{ machines: { id: string }[] }>(request, 'get', '/machines')).machines.length, { timeout: 30_000 })
    .toBeGreaterThan(0)
  const { machines } = await call<{ machines: { id: string }[] }>(request, 'get', '/machines')

  const name = (who: string) => `${who} ${stamp}`
  const agent = async (who: string, options: Record<string, unknown>) =>
    (
      await call<{ agent: { id: string } }>(request, 'post', '/agents', {
        name: name(who),
        machine_id: machines[0].id,
        runtime: 'fake',
        role_card: 'x',
        permission_preset: 'full_auto',
        runtime_options: options,
      })
    ).agent
  // The first member leads.
  const lead = await agent('Lead', {
    tool_calls: [{ tool: 'send_message', args: { text: `@${name('Coder')} 登录页很慢，请查一下原因` } }],
    reply: '已交给 Coder。',
    summary_reply: 'Coder 查到是会话查询慢。',
  })
  const coder = await agent('Coder', { reply: '查过了：会话查询慢。' })
  const { rooms } = await call<{ rooms: { id: string }[] }>(request, 'post', '/projects', {
    name: `dispatch ${stamp}`,
    repo_path: '',
    agent_ids: [lead.id, coder.id],
  })
  const roomId = rooms[0].id
  const done = (turns: number) =>
    expect
      .poll(
        async () => (await call<{ turns: { status: string }[] }>(request, 'get', `/rooms/${roomId}/turns`)).turns.filter((t) => t.status === 'done').length,
        { timeout: 30_000 },
      )
      .toBe(turns)

  // The box says where a message without an @ goes before it is sent.
  await page.goto(`/rooms/${roomId}`)
  await expect(page.getByText(`不带 @ 时交给组长 ${name('Lead')} 分派`)).toBeVisible()
  const box = page.getByRole('textbox', { name: '消息' })
  await box.fill('登录页很慢，谁来看看？')
  await box.press('Enter')

  // The leader takes it and hands it on; Coder, woken by it, answers in
  // the same topic; the leader sums up.
  await done(3)
  await page
    .getByRole('listitem')
    .filter({ hasText: '请查一下原因' })
    .getByRole('button', { name: /已完成 · 3 轮/ })
    .click()
  const topic = page.getByRole('complementary', { name: '话题' })
  await expect(topic.getByText(`由 ${name('Lead')} 唤醒`)).toBeVisible()
  await expect(topic).toContainText('查过了：会话查询慢。')
  await expect(topic).toContainText('Coder 查到是会话查询慢。')
  // The person talks with the leader there: it took their message.
  await expect(topic.getByText(`不带 @ 时由 ${name('Lead')} 回复`)).toBeVisible()

  // The leader switched off, Coder leads: the room's box and the topic's
  // say so.
  const { members } = await call<{ members: { id: string; display_name: string }[] }>(request, 'get', `/rooms/${roomId}/members`)
  const leadMember = members.find((m) => m.display_name === name('Lead'))
  await call(request, 'patch', `/members/${leadMember?.id}`, { enabled: false })
  await page.reload()
  await expect(topic.getByText(`不带 @ 时交给组长 ${name('Coder')}`, { exact: true })).toBeVisible()
  await expect(page.getByText(`不带 @ 时交给组长 ${name('Coder')} 分派`)).toBeVisible()
})
