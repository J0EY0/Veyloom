import { execFileSync } from 'node:child_process'
import { mkdtempSync, realpathSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { expect, test, type APIRequestContext } from '@playwright/test'

// Members in worktrees of their own, as a person meets them (docs/design.md
// 5.21): the first time a member is asked for something, the project's
// leader sets the project up, then the member works in its worktree; its
// work shows on its row in the chat's info and goes onto the main line as
// one commit with the message the person confirms. Two members changing the
// same file are told about in the chat, and marked on both rows. The fake
// runtime plays the agents, writing the files for real.

const api = '/api/v1'

async function call<T>(ctx: APIRequestContext, method: 'get' | 'post', path: string, data?: unknown): Promise<T> {
  const res = method === 'get' ? await ctx.get(api + path) : await ctx.post(api + path, { data })
  expect(res.ok(), `${method.toUpperCase()} ${path} -> ${res.status()} ${await res.text()}`).toBeTruthy()
  return (await res.json()) as T
}

function git(dir: string, ...args: string[]): string {
  return execFileSync('git', args, { cwd: dir, encoding: 'utf8' }).trim()
}

test('a member works in its worktree, and its work is merged onto the main line', async ({ page }) => {
  const request = page.request
  const stamp = Date.now().toString(36)
  const account = { name: 'e2e', password: 'e2e-password' }
  const status = await call<{ setup_required: boolean }>(request, 'get', '/auth/status')
  await call(request, 'post', status.setup_required ? '/auth/setup' : '/auth/login', account)
  await expect
    .poll(async () => (await call<{ machines: { id: string }[] }>(request, 'get', '/machines')).machines.length, { timeout: 30_000 })
    .toBeGreaterThan(0)
  const { machines } = await call<{ machines: { id: string }[] }>(request, 'get', '/machines')

  // A checkout in git, with an .env git ignores.
  const repo = realpathSync(mkdtempSync(join(tmpdir(), 'veyloom-e2e-repo-')))
  git(repo, 'init', '-q', '-b', 'main')
  git(repo, 'config', 'user.name', 'Alice')
  git(repo, 'config', 'user.email', 'alice@example.com')
  writeFileSync(join(repo, '.gitignore'), '.env\n')
  writeFileSync(join(repo, '.env'), 'SECRET=1\n')
  writeFileSync(join(repo, 'README.md'), '# app\n')
  git(repo, 'add', '-A')
  git(repo, 'commit', '-q', '-m', 'first')

  const agent = async (who: string, options: Record<string, unknown>) =>
    (
      await call<{ agent: { id: string } }>(request, 'post', '/agents', {
        name: `${who} ${stamp}`,
        machine_id: machines[0].id,
        runtime: 'fake',
        role_card: 'x',
        permission_preset: 'full_auto',
        runtime_options: options,
      })
    ).agent
  const lead = await agent('Lead', { reply: 'Set up.', tool_calls: [{ tool: 'set_workspace_setup', args: { copy: ['.env'] } }] })
  const coder = await agent('Coder', { reply: 'Done.', write: ['login.go', 'README.md'] })
  const tester = await agent('Tester', { reply: 'Done.', write: ['README.md'] })
  const { rooms } = await call<{ rooms: { id: string }[] }>(request, 'post', '/projects', {
    name: `branches ${stamp}`,
    repo_path: repo,
    agent_ids: [lead.id, coder.id, tester.id],
  })
  const roomId = rooms[0].id
  const { members } = await call<{ members: { id: string; display_name: string }[] }>(request, 'get', `/rooms/${roomId}/members`)
  const idOf = (who: string) => members.find((m) => m.display_name.startsWith(who))?.id ?? ''
  const { user } = await call<{ user: { id: string } }>(request, 'get', '/auth/status')
  const ask = async (who: string, what: string, turns: number) => {
    await call(request, 'post', `/rooms/${roomId}/messages`, {
      user_id: user.id,
      body: `@${who} ${stamp} ${what}`,
      mentions: [{ kind: 'agent', id: idOf(who) }],
    })
    await expect
      .poll(
        async () => (await call<{ turns: { status: string }[] }>(request, 'get', `/rooms/${roomId}/turns`)).turns.filter((t) => t.status === 'done').length,
        {
          timeout: 30_000,
        },
      )
      .toBe(turns)
  }

  // The leader set the project up, then Coder worked in its worktree; then
  // Tester, changing a file Coder changed too.
  await ask('Coder', 'add the login page', 2)
  const { member } = await call<{ member: { work_dir: string; branch: string } }>(request, 'get', `/members/${idOf('Coder')}`)
  expect(member.branch).toMatch(/^veyloom\/coder/)
  await ask('Tester', 'test the login page', 3)

  // The old address of the Branches tab opens the chat's info, where the
  // branches are now.
  await page.goto(`/rooms/${roomId}/branches`)
  await expect(page).toHaveURL(new RegExp(`/rooms/${roomId}\\?panel=members$`))
  const info = page.getByRole('complementary', { name: '群聊信息' })
  await expect(info.getByRole('region', { name: '新工作区的准备步骤' })).toContainText('复制 .env')
  // A member's row, by its name: the rows name each other in their overlaps.
  const rowOf = (who: string) => info.getByRole('listitem').filter({ has: page.locator('[data-slot="item-title"]', { hasText: `${who} ${stamp}` }) })
  const row = rowOf('Coder')
  await expect(row).toContainText('修改了 2 个文件（2 个未提交）')
  await expect(row).toContainText(`与 Tester ${stamp} 修改了相同文件 README.md`)
  await expect(rowOf('Tester')).toContainText(`与 Coder ${stamp} 修改了相同文件 README.md`)
  await expect(rowOf('Lead')).toContainText('直接在项目目录中工作')
  await row.getByRole('button', { name: '合并' }).click()
  const dialog = page.getByRole('dialog', { name: `把 Coder ${stamp} 的改动合并到 main` })
  await expect(dialog.getByLabel('提交说明')).toHaveValue('add the login page')
  await dialog.getByLabel('提交说明').fill('Add the login page')
  await dialog.getByRole('button', { name: '合并到主线' }).click()
  await expect(page.getByText(/已合并到 main（[0-9a-f]{7}）/)).toBeVisible()
  await expect(row).toContainText('没有改动')
  await expect(row.getByRole('button', { name: '合并' })).toHaveCount(0)

  // The main line has it as one commit; the chat says so, and told of the
  // overlap before.
  expect(git(repo, 'log', '-1', '--format=%s')).toBe('Add the login page')
  expect(git(repo, 'show', '--name-only', '--format=', 'HEAD').split('\n')).toEqual(['README.md', 'login.go'])
  await page.getByRole('button', { name: '关闭群聊信息' }).click()
  await expect(page.getByText(`Tester ${stamp}、Coder ${stamp} 都修改了 README.md，后合并的一方可能会有冲突`)).toBeVisible()
  await expect(page.getByText(new RegExp(`Coder ${stamp} 的改动已合并到主线（[0-9a-f]{7}）：Add the login page`))).toBeVisible()
  await expect(page.getByText('初始化项目', { exact: true })).toBeVisible()
})
