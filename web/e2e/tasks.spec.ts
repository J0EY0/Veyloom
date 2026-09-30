import { expect, test, type APIRequestContext } from '@playwright/test'

// The task board, a piece of work's page and the usage page, as a person
// meets them (docs/webui.md 4.20): the lead splits what it was asked
// between two members, each a card saying what it is part of, the lead's
// counting its parts; the work's page draws its turns as a waterfall; the
// usage page counts them and opens the work again. The fake runtime plays
// the agents.

const api = '/api/v1'

async function call<T>(ctx: APIRequestContext, method: 'get' | 'post', path: string, data?: unknown): Promise<T> {
  const res = method === 'get' ? await ctx.get(api + path) : await ctx.post(api + path, { data })
  expect(res.ok(), `${method.toUpperCase()} ${path} -> ${res.status()} ${await res.text()}`).toBeTruthy()
  return (await res.json()) as T
}

test('a piece of work on the board, on its page and in the usage', async ({ page }) => {
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
  const lead = await agent('Lead', {
    reply: '已经拆给 Coder 和 Tester。',
    summary_reply: '标签功能做完了。',
    tool_calls: [
      { tool: 'send_message', args: { to: 'room', title: '实现标签功能', text: `@${name('Coder')} 请实现标签功能` } },
      { tool: 'send_message', args: { to: 'room', title: '补标签功能的测试', text: `@${name('Tester')} 请给标签功能补测试` } },
    ],
  })
  const coder = await agent('Coder', { reply: '实现好了。', delay_ms: 1000 })
  const tester = await agent('Tester', { reply: '测试写好了。', delay_ms: 2000 })
  const project = `tasks ${stamp}`
  const { rooms } = await call<{ rooms: { id: string }[] }>(request, 'post', '/projects', {
    name: project,
    repo_path: '',
    agent_ids: [lead.id, coder.id, tester.id],
  })
  const roomId = rooms[0].id
  const { members } = await call<{ members: { id: string; display_name: string }[] }>(request, 'get', `/rooms/${roomId}/members`)
  const { user } = await call<{ user: { id: string } }>(request, 'get', '/auth/status')
  const leadId = members.find((m) => m.display_name === name('Lead'))?.id ?? ''
  await call(request, 'post', `/rooms/${roomId}/messages`, {
    user_id: user.id,
    body: `@${name('Lead')} 给 linkkeeper 加标签功能`,
    mentions: [{ kind: 'agent', id: leadId }],
  })

  // The board fills as the work goes, and ends done: the lead's card with
  // both its parts, each part saying what it is part of.
  await page.goto(`/rooms/${roomId}/tasks`)
  const done = page.getByRole('region', { name: '已完成' })
  await expect(done.getByRole('heading', { level: 2 })).toHaveText('已完成3', { timeout: 30_000 })
  const leads = done.getByRole('article').filter({ hasText: name('Lead') })
  await expect(leads.getByRole('img', { name: '子任务完成 2/2' })).toBeVisible()
  const coding = done.getByRole('article').filter({ has: page.getByRole('link', { name: '实现标签功能' }) })
  await expect(coding).toContainText('给 linkkeeper 加标签功能')
  await expect(coding).toContainText(name('Coder'))

  // By member, a column each.
  await page.getByRole('tab', { name: '按成员' }).click()
  await expect(page).toHaveURL(/group=member/)
  await expect(page.getByRole('region', { name: name('Tester') }).getByRole('link', { name: '补标签功能的测试' })).toBeVisible()
  await page.getByRole('tab', { name: '按状态' }).click()

  // The work's page: what was asked, its figures, a row a turn.
  await leads.getByRole('link').click()
  await expect(page.getByRole('heading', { level: 1, name: '给 linkkeeper 加标签功能' })).toBeVisible()
  await expect(page.getByRole('term').filter({ hasText: '轮次' }).locator('xpath=following-sibling::dd[1]')).toHaveText('4')
  const rows = page.getByRole('table', { name: '轮次' }).getByRole('rowheader')
  await expect(rows).toHaveCount(4)
  await expect(rows.nth(0)).toContainText('拆分任务')
  await expect(rows.nth(3)).toContainText('汇总答复')
  await page.getByRole('button', { name: /^在话题 #\d+ 中查看/ }).click()
  await expect(page.getByRole('complementary', { name: '话题' })).toContainText('标签功能做完了。')

  // The usage page, down to the project, counts the turns and opens the
  // work again.
  await page.goto(`/usage`)
  await page.getByRole('combobox', { name: '项目' }).click()
  await page.getByRole('option', { name: project, exact: true }).click()
  await expect(page).toHaveURL(/project=/)
  await expect(page.getByRole('group', { name: '总览' }).getByRole('region', { name: '轮次' })).toContainText('4')
  await expect(page.getByRole('region', { name: '成员' }).getByRole('listitem')).toHaveCount(3)
  const works = page.getByRole('region', { name: '按任务' })
  await works.getByRole('link', { name: '给 linkkeeper 加标签功能' }).click()
  await expect(page).toHaveURL(new RegExp(`/rooms/${roomId}/tasks/`))
  await expect(page.getByRole('heading', { level: 1, name: '给 linkkeeper 加标签功能' })).toBeVisible()
})
