import { expect, test, type APIRequestContext } from '@playwright/test'

// Agents waking one another, as a person meets it (docs/design.md 5.22): a
// member hands work to two others at once with its tool, and both start,
// marked with who woke them. Two members that only talk to each other stop
// after three wakes; the person is told in the topic and the inbox, and
// letting them go on wakes the one held back. The project's limit on wakes
// is set in its settings. The fake runtime plays the agents.

const api = '/api/v1'

async function call<T>(ctx: APIRequestContext, method: 'get' | 'post', path: string, data?: unknown): Promise<T> {
  const res = method === 'get' ? await ctx.get(api + path) : await ctx.post(api + path, { data })
  expect(res.ok(), `${method.toUpperCase()} ${path} -> ${res.status()} ${await res.text()}`).toBeTruthy()
  return (await res.json()) as T
}

test('agents wake one another, and wait for the person when they only talk', async ({ page }) => {
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
  const planner = await agent('Planner', {
    reply: '已经分派下去了。',
    summary_reply: '登录页和测试都好了。',
    tool_calls: [{ tool: 'send_message', args: { to: 'room', text: `@${name('Coder')} 请把登录页做出来，@${name('Tester')} 请同时补上测试` } }],
  })
  const coder = await agent('Coder', { reply: '改好了。' })
  const tester = await agent('Tester', { reply: '测试写好了。' })
  // Starter hands on to Ping, and Ping, Pong and Pang hand on round and
  // round: naming the member that handed one the work, or the one a person
  // asked, would only report back to it.
  const starter = await agent('Starter', { reply: `@${name('Ping')} 开始吧` })
  const ping = await agent('Ping', { reply: `@${name('Pong')} 该你了` })
  const pong = await agent('Pong', { reply: `@${name('Pang')} 该你了` })
  const pang = await agent('Pang', { reply: `@${name('Ping')} 该你了` })
  const { rooms } = await call<{ rooms: { id: string }[] }>(request, 'post', '/projects', {
    name: `relays ${stamp}`,
    repo_path: '',
    agent_ids: [planner.id, coder.id, tester.id, starter.id, ping.id, pong.id, pang.id],
  })
  const roomId = rooms[0].id
  const { members } = await call<{ members: { id: string; display_name: string }[] }>(request, 'get', `/rooms/${roomId}/members`)
  const idOf = (who: string) => members.find((m) => m.display_name === name(who))?.id ?? ''
  const { user } = await call<{ user: { id: string } }>(request, 'get', '/auth/status')
  const ask = async (who: string, what: string) =>
    call(request, 'post', `/rooms/${roomId}/messages`, { user_id: user.id, body: `@${name(who)} ${what}`, mentions: [{ kind: 'agent', id: idOf(who) }] })
  const done = (turns: number) =>
    expect
      .poll(
        async () => (await call<{ turns: { status: string }[] }>(request, 'get', `/rooms/${roomId}/turns`)).turns.filter((t) => t.status === 'done').length,
        { timeout: 30_000 },
      )
      .toBe(turns)

  // Planner hands the work to two members at once: both work in the topic
  // its message opens, each marked as woken by Planner. Once both are done,
  // Planner sums it up for the person, in the topic they asked it in.
  await ask('Planner', '把登录页的活分下去')
  await done(4)
  await page.goto(`/rooms/${roomId}`)
  await page
    .getByRole('listitem')
    .filter({ hasText: '请把登录页做出来' })
    .getByRole('button', { name: /完成 · 2 轮/ })
    .click()
  const topic = page.getByRole('complementary', { name: '话题' })
  await expect(topic.getByText(`由 ${name('Planner')} 叫醒`)).toHaveCount(2)
  await expect(topic).toContainText('改好了。')
  await expect(topic).toContainText('测试写好了。')
  await page.keyboard.press('Escape')
  // The answer to the person counts the whole piece of work: Planner's two
  // turns, Coder's and Tester's.
  await page
    .getByRole('listitem')
    .filter({ hasText: '已经分派下去了。' })
    .getByRole('button', { name: /完成 · 4 轮/ })
    .click()
  await expect(topic.getByText(new RegExp(`做完了 ${name('Planner')} 交出去的活，交回给 ${name('Planner')}`))).toBeVisible()
  await expect(topic).toContainText(`${account.name} 登录页和测试都好了。`)
  await page.keyboard.press('Escape')

  // Ping, Pong and Pang only talk: after three wakes the fourth waits for
  // the person, who hears of it in the inbox.
  await ask('Starter', '让他们俩接着聊')
  await done(8)
  await page.goto('/inbox')
  const forMe = page.getByRole('main')
  const idle = `${name('Pang')} 想叫醒 ${name('Ping')}，但最近 3 轮被叫醒的都只说话、没干活，等你决定`
  const held = forMe.getByRole('link').filter({ hasText: idle })
  await expect(held).toContainText('Veyloom')
  await held.click()
  const picked = forMe.getByRole('complementary', { name: '话题' })
  await expect(picked.getByText(`由 ${name('Ping')} 叫醒`).first()).toBeVisible()

  // Let go on, Ping is woken in a piece of work of its own: it hands on to
  // Pong, Pong to Pang, whose answer naming Ping reports back, and Ping
  // sums it up.
  await picked.getByRole('button', { name: '继续', exact: true }).click()
  await expect(picked.getByText('已继续')).toBeVisible()
  await done(12)
  await expect(picked.getByText(`${name('Pong')}、${name('Pang')} 做完了 ${name('Ping')} 交出去的活，交回给 ${name('Ping')}`)).toBeVisible()

  // A limit of one wake, set in the project's settings: Starter wakes Ping,
  // and Ping waking Pong waits for the person.
  await page.goto(`/rooms/${roomId}?panel=members`)
  await page.getByRole('button', { name: `编辑项目 relays ${stamp}` }).click()
  const settings = page.getByRole('dialog', { name: '编辑项目' })
  await settings.getByLabel('agent 互相叫醒的上限').fill('1')
  await settings.getByRole('button', { name: '保存' }).click()
  await expect(settings).toHaveCount(0)
  await ask('Starter', '再来一次')
  await done(14)
  const { turns } = await call<{ turns: { thread_id: string }[] }>(request, 'get', `/rooms/${roomId}/turns`)
  await page.goto(`/rooms/${roomId}?thread=${turns[0].thread_id}`)
  await expect(topic.getByText(`${name('Ping')} 想叫醒 ${name('Pong')}，但这件事自你上次说话以来 agent 已经互相叫醒了 1 轮，等你决定`)).toBeVisible()
  await expect(topic.getByRole('button', { name: '继续', exact: true })).toBeVisible()
})
