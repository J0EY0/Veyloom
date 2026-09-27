import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import type { Branches } from '@/api/types'
import { setCurrentUser } from '@/lib/currentUser'
import { branches, renderView, rowOf, stub } from './branchesTesting'

const posted = (sink: { body?: unknown }) => async (req: Request) => {
  sink.body = await req.json()
  return Response.json(
    { message: { id: 'x', room_id: 'r1', sender_kind: 'user', body: '', mentions: [], created_at: '2026-09-23T01:00:00Z', seq: 1 } },
    { status: 201 },
  )
}

// The branches in the chat's info (docs/design.md 5.21): the main line and
// the checkout on the project's card, each member's worktree on its row.
describe('branches in the chat’s info', () => {
  beforeEach(() => setCurrentUser({ id: 'u1', name: 'alice' }))
  afterEach(() => setCurrentUser(null))

  it('shows the main line, the checkout’s own changes, how worktrees are got ready, and each member’s branch', async () => {
    stub()
    renderView()
    const panel = screen.getByRole('complementary', { name: '群聊信息' })
    expect(await within(panel).findByText('/src/app')).toBeInTheDocument()
    await waitFor(() => expect(within(panel).getByText('main').parentElement).toHaveTextContent('主线 main'))
    // What was changed in the checkout and not committed, to read or commit.
    const checkout = await screen.findByRole('region', { name: '仓库目录里有 1 个文件没提交' })
    expect(within(checkout).getByRole('button', { name: '看改动' })).toBeInTheDocument()
    expect(within(checkout).getByRole('button', { name: '提交…' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: '新工作区的准备步骤' })).toHaveTextContent('新工作区的准备步骤：复制 .env、docs/ · 运行 npm ci')

    const coder = await rowOf('Coder')
    await waitFor(() => expect(coder).toHaveTextContent('veyloom/coder+12 −1改了 2 个文件（1 个没提交） · 落后 1'))
    // Both rows of an overlap say so, each naming the other.
    expect(coder).toHaveTextContent('和 Tester 都改了 src/api.ts')
    expect(await rowOf('Tester')).toHaveTextContent('和 Coder 都改了 src/api.ts')
    expect(await rowOf('Writer')).toHaveTextContent('还没有工作区，第一次开工时建')
    expect(await rowOf('Writer')).not.toHaveTextContent('都改了')
    // The leader works in the checkout itself.
    expect(await rowOf('Lead')).toHaveTextContent('在项目目录里干活')
    // Nothing is done to a worktree while its member works.
    expect(within(await rowOf('Tester')).getByRole('button', { name: '合并' })).toBeDisabled()
    expect(within(coder).getByRole('button', { name: '合并' })).toBeEnabled()
  })

  it('merges a member’s work, naming the work on its branch and the files another member changed too', async () => {
    let merged: unknown
    stub({
      '/members/m1/merge': async (req: Request) => {
        merged = await req.json()
        return { merge: { commit: 'abcdef123456' } }
      },
    })
    const user = userEvent.setup()
    renderView()
    const coder = await rowOf('Coder')
    await user.click(await within(coder).findByRole('button', { name: '合并' }))
    const dialog = await screen.findByRole('dialog', { name: '把 Coder 的改动合并到 main' })
    expect(await within(dialog).findByText('veyloom/coder 上是这 1 件事的改动，合并时压成一个提交')).toBeInTheDocument()
    expect(within(dialog).getByText('给 delete 加 --tag').closest('li')).toHaveTextContent('#12给 delete 加 --tag')
    expect(within(dialog).getByText('Tester 也改了 src/api.ts，后合并的那个可能会冲突')).toBeInTheDocument()
    expect(within(dialog).getByText('src/new.ts')).toBeInTheDocument()
    const message = within(dialog).getByLabelText('提交说明')
    expect(message).toHaveValue('add the feature')
    await user.clear(message)
    await user.type(message, 'Add the feature (Coder)')
    await user.click(within(dialog).getByRole('button', { name: '合并到主线' }))
    await waitFor(() => expect(merged).toEqual({ message: 'Add the feature (Coder)', leave: [] }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  })

  it('reads a member’s changes over the merge, the message kept', async () => {
    stub({ '/members/m1/diff': { patch: '--- a/src/api.ts\n+++ b/src/api.ts\n' } })
    const user = userEvent.setup()
    renderView()
    await user.click(await within(await rowOf('Coder')).findByRole('button', { name: '合并' }))
    const dialog = await screen.findByRole('dialog', { name: '把 Coder 的改动合并到 main' })
    await user.type(within(dialog).getByLabelText('提交说明'), ' (kept)')
    await user.click(within(dialog).getByRole('button', { name: '看改动' }))
    const diff = await screen.findByRole('dialog', { name: 'Coder 的改动' })
    await user.keyboard('{Escape}')
    await waitFor(() => expect(diff).not.toBeInTheDocument())
    expect(screen.getByRole('dialog', { name: '把 Coder 的改动合并到 main' })).toBeInTheDocument()
    expect(within(dialog).getByLabelText('提交说明')).toHaveValue('add the feature (kept)')
  })

  it('hands conflicts to the member, in the chat', async () => {
    const sink: { body?: unknown } = {}
    stub({ '/members/m1/merge': { merge: { conflicts: ['src/api.ts'] } }, '/rooms/r1/messages': posted(sink) })
    const user = userEvent.setup()
    renderView()
    await user.click(await within(await rowOf('Coder')).findByRole('button', { name: '合并' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '合并到主线' }))
    expect(await within(dialog).findByText('Coder 的改动和主线在这些文件上冲突：')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: '交给 Coder 解决' }))
    await waitFor(() =>
      expect(sink.body).toMatchObject({
        body: '@Coder 你的分支和主线 main 在这些文件上冲突了：src/api.ts。请把 main 合并进你的分支，解决冲突后告诉我。',
        mentions: [{ kind: 'agent', id: 'm1' }],
      }),
    )
  })

  it('brings the main line into a branch from the member’s menu, and hands over what conflicts', async () => {
    const sink: { body?: unknown } = {}
    stub({ '/members/m1/sync': { sync: { conflicts: ['src/api.ts'] } }, '/rooms/r1/messages': posted(sink) })
    const user = userEvent.setup()
    renderView()
    const coder = await rowOf('Coder')
    await within(coder).findByRole('button', { name: '合并' })
    await user.click(within(coder).getByRole('button', { name: '更多' }))
    const menu = await screen.findByRole('menu')
    expect(within(menu).getAllByRole('group')[0]).toHaveTextContent('分支看改动同步主线重置到主线…')
    await user.click(within(menu).getByRole('menuitem', { name: '同步主线' }))
    const dialog = await screen.findByRole('dialog', { name: '同步时有冲突 · Coder' })
    await user.click(within(dialog).getByRole('button', { name: '交给 Coder 解决' }))
    await waitFor(() => expect(sink.body).toMatchObject({ mentions: [{ kind: 'agent', id: 'm1' }] }))
  })

  it('shows a merge a member left under way, and hands it back or gives it up', async () => {
    const sink: { body?: unknown } = {}
    let aborted = false
    const stuck: Branches = {
      ...branches,
      members: [{ ...branches.members[0], status: { ...branches.members[0].status!, merging: true, conflicts: ['src/api.ts'] } }],
      overlaps: [],
    }
    stub(
      {
        '/members/m1/merge/abort': () => {
          aborted = true
          return new Response(null, { status: 204 })
        },
        '/rooms/r1/messages': posted(sink),
      },
      {},
      stuck,
    )
    const user = userEvent.setup()
    renderView()
    const coder = await rowOf('Coder')
    await waitFor(() => expect(coder).toHaveTextContent('合并停在冲突上 src/api.ts'))
    // Nothing goes onto the main line meanwhile.
    expect(within(coder).queryByRole('button', { name: '合并' })).toBeNull()

    await user.click(within(coder).getByRole('button', { name: '处理冲突' }))
    let dialog = await screen.findByRole('dialog', { name: 'Coder 的工作区里合并还没完成' })
    expect(dialog).toHaveTextContent('还有冲突标记：src/api.ts')
    await user.click(within(dialog).getByRole('button', { name: '再交给 Coder' }))
    await waitFor(() =>
      expect(sink.body).toMatchObject({
        body: '@Coder 你的工作区里合并 main 还没完成：src/api.ts。请解决冲突后提交，然后告诉我。',
        mentions: [{ kind: 'agent', id: 'm1' }],
      }),
    )
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())

    await user.click(within(coder).getByRole('button', { name: '处理冲突' }))
    dialog = await screen.findByRole('dialog', { name: 'Coder 的工作区里合并还没完成' })
    await user.click(within(dialog).getByRole('button', { name: '放弃这次合并' }))
    const confirm = await screen.findByRole('alertdialog', { name: '放弃 Coder 的这次合并？' })
    expect(aborted).toBe(false)
    await user.click(within(confirm).getByRole('button', { name: '放弃这次合并' }))
    await waitFor(() => expect(aborted).toBe(true))
  })

  it('adopts the steps the leader wrote down', async () => {
    let settled: unknown
    stub(
      {
        '/projects/p1/workspace/pending': async (req: Request) => {
          settled = await req.json()
          return new Response(null, { status: 204 })
        },
      },
      { workspace_pending: { copy: ['.env'], run: 'make setup' } },
    )
    const user = userEvent.setup()
    renderView()
    expect(await screen.findByText('组长写好了准备步骤，命令要你采用后才会执行')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '采用' }))
    await waitFor(() => expect(settled).toEqual({ adopt: true }))
  })

  it('writes the steps down in a dialog', async () => {
    let patched: unknown
    stub({
      '/projects/p1': async (req: Request) => {
        patched = await req.json()
        return { project: { id: 'p1' }, rooms: [] }
      },
    })
    const user = userEvent.setup()
    renderView()
    await user.click(await screen.findByRole('button', { name: '编辑步骤' }))
    const dialog = await screen.findByRole('dialog', { name: '编辑准备步骤' })
    const copy = within(dialog).getByLabelText('从仓库目录复制（每行一个）')
    expect(copy).toHaveValue('.env\ndocs/')
    await user.clear(copy)
    await user.type(copy, '.env{Enter} config/local.json ')
    await user.clear(within(dialog).getByLabelText('准备命令'))
    await user.type(within(dialog).getByLabelText('准备命令'), 'pnpm i')
    await user.click(within(dialog).getByRole('button', { name: '保存' }))
    await waitFor(() => expect(patched).toEqual({ workspace_steps: { copy: ['.env', 'config/local.json'], run: 'pnpm i' } }))
  })

  it('shows no branches where the project has no worktrees', async () => {
    const calls = stub({}, {}, { main: { repo_path: '/src/app', git: false }, members: [] })
    renderView()
    await rowOf('Coder')
    await waitFor(() => expect(calls).toContain('GET /projects/p1/branches'))
    expect(screen.queryByRole('button', { name: '编辑步骤' })).toBeNull()
    expect(await rowOf('Lead')).not.toHaveTextContent('在项目目录里干活')
    expect(screen.queryByRole('button', { name: '合并' })).toBeNull()
  })
})
