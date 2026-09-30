import { expect, test, type APIRequestContext } from '@playwright/test'

// The wiki maintainer, as a person meets it (docs/design.md 5.12, 5.21):
// once there is something to go over, the Wiki tab offers to have the
// wiki kept, by the leader unless another member is picked; one click
// turns it on, another runs an upkeep now; the upkeep runs in the
// project's wiki topic, and what it wrote is in the wiki. The fake runtime
// plays the agents.

const api = '/api/v1'

async function call<T>(ctx: APIRequestContext, method: 'get' | 'post', path: string, data?: unknown): Promise<T> {
  const res = method === 'get' ? await ctx.get(api + path) : await ctx.post(api + path, { data })
  expect(res.ok(), `${method.toUpperCase()} ${path} -> ${res.status()} ${await res.text()}`).toBeTruthy()
  return (await res.json()) as T
}

test('a member keeps the wiki: offered, turned on, run, written', async ({ page }) => {
  const request = page.request
  const stamp = Date.now().toString(36)
  const account = { name: 'e2e', password: 'e2e-password' }
  const status = await call<{ setup_required: boolean }>(request, 'get', '/auth/status')
  await call(request, 'post', status.setup_required ? '/auth/setup' : '/auth/login', account)
  await expect
    .poll(async () => (await call<{ machines: { id: string }[] }>(request, 'get', '/machines')).machines.length, { timeout: 30_000 })
    .toBeGreaterThan(0)
  const { machines } = await call<{ machines: { id: string }[] }>(request, 'get', '/machines')

  const agent = async (who: string, options: Record<string, unknown>) =>
    (
      await call<{ agent: { id: string } }>(request, 'post', '/agents', {
        name: `${who} ${stamp}`,
        machine_id: machines[0].id,
        runtime: 'fake',
        role_card: 'x',
        permission_preset: 'read_only',
        runtime_options: options,
      })
    ).agent
  const title = `Staging listens on 6544 ${stamp}`
  const coder = await agent('Coder', { reply: 'Staging listens on 6544.' })
  const keeper = await agent('Keeper', {
    reply: '整理好了。',
    tool_calls: [
      { tool: 'list_turns', args: {} },
      { tool: 'write_wiki', args: { type: 'Fact', slug: `staging-port-${stamp}`, title, description: 'Where staging listens.', body: 'Port 6544.' } },
    ],
  })
  const { rooms } = await call<{ rooms: { id: string }[] }>(request, 'post', '/projects', {
    name: `keep ${stamp}`,
    repo_path: '',
    agent_ids: [coder.id, keeper.id],
  })
  const roomId = rooms[0].id
  const { members } = await call<{ members: { id: string; display_name: string }[] }>(request, 'get', `/rooms/${roomId}/members`)
  const coderMember = members.find((m) => m.display_name.startsWith('Coder'))?.id ?? ''
  await call(request, 'post', `/rooms/${roomId}/messages`, { body: '@Coder where does staging listen?', mentions: [{ kind: 'agent', id: coderMember }] })

  // Something to go over: the Wiki tab offers a maintainer.
  await page.goto(`/rooms/${roomId}/wiki`)
  const offer = page.getByRole('region', { name: '要指定一个成员维护这个 wiki 吗？' })
  await expect(offer).toBeVisible()
  // Coder joined first, and so leads, and would keep it; Keeper is picked.
  const who = offer.getByRole('combobox', { name: '由谁整理' })
  await expect(who).toHaveText(`组长（Coder ${stamp}）`)
  await who.click()
  await page.getByRole('option', { name: `Keeper ${stamp}`, exact: true }).click()
  await offer.getByRole('button', { name: '开启（每天一次）' }).click()

  // Daily by default, and changed right on the card (docs/design.md 5.16).
  const card = page.getByRole('region', { name: `Keeper ${stamp} 在维护这个 wiki` })
  await expect(card.getByRole('combobox', { name: '整理时机' })).toHaveText('每天一次')
  await expect(card).toContainText('待整理：本群 1 轮')
  await card.getByRole('button', { name: '现在整理' }).click()
  await expect(card).toContainText('上次整理于')
  await expect(card).toContainText('没有待整理的内容')
  await expect(page.getByLabel('页面').getByRole('link', { name: title })).toBeVisible()

  // The upkeep ran in the wiki topic, said so, and is marked as one.
  await card.getByRole('button', { name: '打开整理话题' }).click()
  const topic = page.getByRole('complementary', { name: '话题' })
  await expect(topic.getByRole('heading', { name: /Wiki 整理/ })).toBeVisible()
  await expect(topic).toContainText(`Keeper ${stamp} 开始整理 wiki（应要求整理）：本群 1 轮，其他项目使用本团队技能 0 轮`)
  await expect(topic).toContainText('整理 wiki')
  await expect(topic).toContainText('整理好了。')
})
