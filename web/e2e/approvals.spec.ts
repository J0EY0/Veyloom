import { tmpdir } from 'node:os'
import { expect, test, type APIRequestContext } from '@playwright/test'

// Fewer approvals, as a person meets them (docs/design.md 4.6): the menu
// beside "allow" lets the rest of a turn through, a strip atop the topic
// saying so while it runs; or keeps the like of a request for the member,
// who is not asked about it again until the rule is taken back in its
// settings. The fake runtime plays the agents, one project each.

const api = '/api/v1'

async function call<T>(ctx: APIRequestContext, method: 'get' | 'post', path: string, data?: unknown): Promise<T> {
  const res = method === 'get' ? await ctx.get(api + path) : await ctx.post(api + path, { data })
  expect(res.ok(), `${method.toUpperCase()} ${path} -> ${res.status()} ${await res.text()}`).toBeTruthy()
  return (await res.json()) as T
}

test('the rest of a turn let through, and a command allowed always', async ({ page }) => {
  const request = page.request
  const stamp = Date.now().toString(36)
  const account = { name: 'e2e', password: 'e2e-password' }
  const status = await call<{ setup_required: boolean }>(request, 'get', '/auth/status')
  await call(request, 'post', status.setup_required ? '/auth/setup' : '/auth/login', account)
  await expect
    .poll(async () => (await call<{ machines: { id: string }[] }>(request, 'get', '/machines')).machines.length, { timeout: 30_000 })
    .toBeGreaterThan(0)
  const { machines } = await call<{ machines: { id: string }[] }>(request, 'get', '/machines')

  // A project of one member, which asks as its options say.
  const project = async (who: string, options: Record<string, unknown>) => {
    const { agent } = await call<{ agent: { id: string } }>(request, 'post', '/agents', {
      name: `${who} ${stamp}`,
      machine_id: machines[0].id,
      runtime: 'fake',
      role_card: 'Ask first.',
      permission_preset: 'edit_with_approval',
      runtime_options: options,
    })
    const made = await call<{ rooms: { id: string }[] }>(request, 'post', '/projects', { name: `${who} ${stamp}`, repo_path: tmpdir(), agent_ids: [agent.id] })
    return made.rooms[0].id
  }
  // The chat's own box, not a topic's that may be open beside it.
  const ask = async (text: string) => {
    await page.getByPlaceholder(/说点什么/).fill(text)
    await page.keyboard.press('Enter')
  }
  const topic = page.getByRole('complementary', { name: '话题' })

  // It asks three times and takes a while after. Allowed with the rest of
  // the turn, the other two go through unasked, each a line with a shield,
  // counted on the strip until the turn is over.
  await page.goto(`/rooms/${await project('Trusting', { approval: true, approvals: 3, similar: ['Bash(make test:*)'], delay_ms: 4000, reply: '审完了。' })}`)
  await ask('审一下')
  await page.getByRole('button', { name: /在等你审批/ }).click()
  await topic.getByRole('button', { name: '允许的范围' }).click()
  const menu = page.getByRole('menu', { name: '允许的范围' })
  await expect(menu.getByRole('menuitem', { name: '本轮允许 make test *' })).toBeVisible()
  await expect(menu.getByRole('menuitem', { name: '始终允许 make test *' })).toBeVisible()
  await menu.getByRole('menuitem', { name: '本轮自动批准' }).click()
  const strip = topic.getByRole('status', { name: '本轮自动批准' })
  await expect(strip).toContainText('已放行 2 次', { timeout: 20_000 })
  await expect(strip).toHaveCount(0, { timeout: 20_000 })
  await expect(topic.getByRole('button', { name: /运行了.*本轮自动批准/ })).toHaveCount(2)
  await expect(page.getByRole('listitem').filter({ hasText: 'Allowed 3 of 3' })).toHaveCount(1)

  // It asks as Codex does, offering the words its command starts with.
  // Allowed always, it is not asked again; the rule is in its settings.
  const keeper = `Keeper ${stamp}`
  const room = await project('Keeper', { approval: true, prefix: ['make', 'test'] })
  await page.goto(`/rooms/${room}`)
  await ask('跑测试')
  await page.getByRole('button', { name: /在等你审批/ }).click()
  await topic.getByRole('button', { name: '允许的范围' }).click()
  await page.getByRole('menu', { name: '允许的范围' }).getByRole('menuitem', { name: '始终允许 make test *' }).click()
  await expect(page.getByRole('listitem').filter({ hasText: 'Allowed, and the like of it' })).toHaveCount(1, { timeout: 20_000 })
  await ask('再跑一次')
  await expect(page.getByRole('listitem').filter({ hasText: 'Allowed by a rule' })).toHaveCount(1, { timeout: 20_000 })

  // Taken back, the member is asked from its next turn on.
  await page.goto(`/rooms/${room}?panel=members`)
  await page
    .getByRole('complementary', { name: '群聊信息' })
    .getByRole('button', { name: new RegExp(`^${keeper} 组长`) })
    .click()
  const settings = page.getByRole('dialog', { name: `编辑 ${keeper}` })
  const rules = settings.getByRole('list', { name: '始终允许的命令' })
  await expect(rules).toContainText('make test *')
  await expect(settings.getByRole('radio', { name: '跟随 Agent' })).toBeChecked()
  await rules.getByRole('button', { name: '撤销 make test *' }).click()
  await expect(settings.getByText('在审批里选“始终允许”后会列在这里')).toBeVisible()
  await settings.getByRole('button', { name: '取消' }).click()
  await page.goto(`/rooms/${room}`)
  await ask('第三次')
  await expect(page.getByRole('button', { name: /在等你审批/ })).toBeVisible({ timeout: 20_000 })
})
