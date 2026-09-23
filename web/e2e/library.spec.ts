import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { expect, test, type APIRequestContext } from '@playwright/test'

// The skill library, as a person meets it (docs/design.md 5.15, webui.md
// 4.10): a person imports a skill from a folder, looked after by a
// project's team, finds it in the list of skills, and installs it for two
// agents. One improves it in its task, which puts it on
// trial; the other's turn uses it and is recorded on the skill's page; the
// person rolls the change back. The fake runtime plays the agents.

const api = '/api/v1'

async function call<T>(ctx: APIRequestContext, method: 'get' | 'post', path: string, data?: unknown): Promise<T> {
  const res = method === 'get' ? await ctx.get(api + path) : await ctx.post(api + path, { data })
  expect(res.ok(), `${method.toUpperCase()} ${path} -> ${res.status()} ${await res.text()}`).toBeTruthy()
  return (await res.json()) as T
}

test('a skill imported, installed, improved on trial, used and rolled back', async ({ page }) => {
  const request = page.request
  const stamp = Date.now().toString(36)
  const account = { name: 'e2e', password: 'e2e-password' }
  const status = await call<{ setup_required: boolean }>(request, 'get', '/auth/status')
  await call(request, 'post', status.setup_required ? '/auth/setup' : '/auth/login', account)
  await expect
    .poll(async () => (await call<{ machines: { id: string }[] }>(request, 'get', '/machines')).machines.length, { timeout: 30_000 })
    .toBeGreaterThan(0)
  const { machines } = await call<{ machines: { id: string }[] }>(request, 'get', '/machines')

  // The skill, in a folder of its own on this machine, as a runtime keeps it.
  const name = `table-tests-${stamp}`
  const title = `Table tests ${stamp}`
  const folder = join(mkdtempSync(join(tmpdir(), 'veyloom-skill-')), name)
  mkdirSync(folder)
  writeFileSync(
    join(folder, 'SKILL.md'),
    `---\nname: ${name}\ndescription: Use when a function needs several test cases.\n---\n\n# ${title}\n\nWrite the cases as a table.\n`,
  )

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
  const improver = await agent('Improver', {
    reply: '改好了。',
    tool_calls: [
      {
        tool: 'patch_wiki',
        args: {
          scope: 'library',
          path: `/skills/${name}/SKILL.md`,
          reason: 'the cases need names',
          edits: [{ op: 'append', content: 'Name each case after what it checks.' }],
        },
      },
    ],
  })
  const tester = await agent('Tester', { use_skill: name })
  const projectName = `library ${stamp}`
  const { rooms } = await call<{ rooms: { id: string }[] }>(request, 'post', '/projects', {
    name: projectName,
    repo_path: '',
    agent_ids: [improver.id, tester.id],
  })
  const roomId = rooms[0].id
  const { members } = await call<{ members: { id: string; display_name: string }[] }>(request, 'get', `/rooms/${roomId}/members`)
  const member = (who: string) => members.find((m) => m.display_name.startsWith(who))?.id ?? ''

  // A person imports it, looked after by the project's team.
  await page.goto('/library')
  await page.getByRole('button', { name: '导入技能' }).click()
  const dialog = page.getByRole('dialog', { name: '从文件夹导入技能' })
  await dialog.getByLabel('文件夹').fill(folder)
  await dialog.getByLabel('负责的团队').click()
  await page.getByRole('option', { name: projectName }).click()
  await dialog.getByRole('button', { name: '导入' }).click()
  await expect(page.getByRole('heading', { name: title })).toBeVisible()
  await expect(page.getByText(`由「${projectName}」团队负责`)).toBeVisible()

  // Back in the list, its row says whose it is.
  await page.getByRole('article').getByRole('link', { name: '技能库' }).click()
  const row = page
    .getByRole('list', { name: '技能' })
    .getByRole('listitem')
    .filter({ has: page.getByRole('link', { name: title }) })
  await expect(row).toContainText(`「${projectName}」团队`)
  await row.getByRole('link', { name: title }).click()
  await expect(page.getByRole('heading', { name: title })).toBeVisible()

  // And installs it for both agents.
  await page.getByRole('button', { name: '装给…' }).click()
  await page.getByRole('menuitemcheckbox', { name: new RegExp(`Improver ${stamp}`) }).click()
  await expect(page.getByText(`装给了 Improver ${stamp}`)).toBeVisible()
  await page.getByRole('menuitemcheckbox', { name: new RegExp(`Tester ${stamp}`) }).click()
  await expect(page.getByText(`装给了 Improver ${stamp}、Tester ${stamp}`)).toBeVisible()
  await page.keyboard.press('Escape')

  // Improved in a task, at once, on trial.
  await call(request, 'post', `/rooms/${roomId}/messages`, { body: '@Improver write the tests', mentions: [{ kind: 'agent', id: member('Improver') }] })
  const trialOf = async () =>
    (await call<{ page: { trial?: { status: string } } }>(request, 'get', `/library/page?path=/skills/${name}/SKILL.md`)).page.trial?.status
  await expect.poll(trialOf).toBe('open')
  // The list says it is on trial.
  await page.goto('/library')
  await expect(row).toContainText('试用中')
  await row.getByRole('link', { name: title }).click()

  // Used in another turn, which the page records.
  await call(request, 'post', `/rooms/${roomId}/messages`, { body: '@Tester write the tests', mentions: [{ kind: 'agent', id: member('Tester') }] })
  await expect.poll(async () => (await call<{ uses: unknown[] }>(request, 'get', `/library/usage?name=${name}`)).uses.length).toBe(1)
  await page.reload()
  const trial = page.getByRole('region', { name: '试用' })
  await expect(trial).toContainText('试用中 · 已用 1/3 轮')
  await expect(page.getByText('Name each case after what it checks.')).toBeVisible()
  await expect(page.getByRole('link', { name: `${projectName} · 话题 #2` })).toBeVisible()

  // The person rolls the change back, saying why.
  await trial.getByRole('button', { name: '退回…' }).click()
  const ask = page.getByRole('alertdialog', { name: '退回这次改动？' })
  await ask.getByLabel('原因（可不填）').fill('名字太长')
  await ask.getByRole('button', { name: '退回' }).click()
  await expect(page.getByText(/上次改动已被 e2e 退回：名字太长/)).toBeVisible()
  await expect(page.getByText('Name each case after what it checks.')).toHaveCount(0)
})
