import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import type { Branches } from '@/api/types'
import { setCurrentUser } from '@/lib/currentUser'
import { app, branches, renderView, stub } from './branchesTesting'

describe('BranchesView', () => {
  beforeEach(() => setCurrentUser({ id: 'u1', name: 'alice' }))
  afterEach(() => setCurrentUser(null))

  it('shows the main line, how worktrees are got ready, and each member against it, marked with the files another changed too', async () => {
    stub()
    renderView()
    expect(await screen.findByRole('heading', { name: /主线\s*main/ })).toBeInTheDocument()
    expect(screen.getByText('/src/app')).toBeInTheDocument()
    // What was changed in the checkout and not committed, to read or commit.
    const checkout = screen.getByRole('region', { name: '仓库目录里有 1 个文件没提交' })
    expect(checkout).toHaveTextContent('notes.md')
    expect(within(checkout).getByRole('button', { name: '提交…' })).toBeInTheDocument()
    const setup = screen.getByRole('region', { name: '新工作区的准备步骤' })
    expect(setup).toHaveTextContent('新工作区的准备步骤：复制 .env、docs/ · 运行 npm ci')

    const rows = screen.getByRole('region', { name: '员工的分支' })
    expect(within(rows).getByText('Coder').closest('[data-slot="item"]')).toHaveTextContent('veyloom/coder+12 −1领先 2 · 落后 1 · 改了 2 个文件（1 个没提交）')
    expect(within(rows).getByText('Writer').closest('[data-slot="item"]')).toHaveTextContent('还没有工作区，第一次开工时建')
    // Both rows of an overlap say so, each naming the other.
    const coder = within(rows).getByText('Coder').closest('[data-slot="item"]') as HTMLElement
    const tester = within(rows).getByText('Tester').closest('[data-slot="item"]') as HTMLElement
    expect(coder).toHaveTextContent('和 Tester 都改了 src/api.ts')
    expect(tester).toHaveTextContent('和 Coder 都改了 src/api.ts')
    expect(within(rows).getByText('Writer').closest('[data-slot="item"]')).not.toHaveTextContent('都改了')
    // Nothing is done to a worktree while its member works.
    expect(within(tester).getByRole('button', { name: '合并到主线' })).toBeDisabled()
  })

  it('merges a member’s work with the message a person confirms', async () => {
    let merged: unknown
    stub({
      '/members/m1/merge': async (req: Request) => {
        merged = await req.json()
        return { merge: { commit: 'abcdef123456' } }
      },
    })
    const user = userEvent.setup()
    renderView()
    const coder = (await screen.findByText('Coder')).closest('[data-slot="item"]') as HTMLElement
    await user.click(within(coder).getByRole('button', { name: '合并到主线' }))
    const dialog = await screen.findByRole('dialog', { name: '把 Coder 的改动合并到 main' })
    expect(within(dialog).getByText('src/new.ts')).toBeInTheDocument()
    const message = within(dialog).getByLabelText('提交说明')
    expect(message).toHaveValue('add the feature')
    await user.clear(message)
    await user.type(message, 'Add the feature (Coder)')
    await user.click(within(dialog).getByRole('button', { name: '合并到主线' }))
    await waitFor(() => expect(merged).toEqual({ message: 'Add the feature (Coder)', leave: [] }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  })

  it('hands conflicts to the member, in the chat', async () => {
    let posted: unknown
    stub({
      '/members/m1/merge': { merge: { conflicts: ['src/api.ts'] } },
      '/rooms/r1/messages': async (req: Request) => {
        posted = await req.json()
        return Response.json(
          { message: { id: 'x', room_id: 'r1', sender_kind: 'user', body: '', mentions: [], created_at: '2026-09-23T01:00:00Z', seq: 1 } },
          { status: 201 },
        )
      },
    })
    const user = userEvent.setup()
    renderView()
    const coder = (await screen.findByText('Coder')).closest('[data-slot="item"]') as HTMLElement
    await user.click(within(coder).getByRole('button', { name: '合并到主线' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '合并到主线' }))
    expect(await within(dialog).findByText('Coder 的改动和主线在这些文件上冲突：')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: '交给 Coder 解决' }))
    await waitFor(() =>
      expect(posted).toMatchObject({
        body: '@Coder 你的分支和主线 main 在这些文件上冲突了：src/api.ts。请把 main 合并进你的分支，解决冲突后告诉我。',
        mentions: [{ kind: 'agent', id: 'm1' }],
      }),
    )
  })

  it('shows a merge a member left under way, and hands it back or gives it up', async () => {
    let posted: unknown
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
        '/rooms/r1/messages': async (req: Request) => {
          posted = await req.json()
          return Response.json(
            { message: { id: 'x', room_id: 'r1', sender_kind: 'user', body: '', mentions: [], created_at: '2026-09-24T01:00:00Z', seq: 1 } },
            { status: 201 },
          )
        },
      },
      {},
      stuck,
    )
    const user = userEvent.setup()
    renderView()
    const coder = (await screen.findByText('Coder')).closest('[data-slot="item"]') as HTMLElement
    expect(coder).toHaveTextContent('合并停在冲突上')
    expect(coder).toHaveTextContent('还有冲突标记： src/api.ts')
    // Nothing goes onto the main line, or comes in, meanwhile.
    expect(within(coder).queryByRole('button', { name: '合并到主线' })).toBeNull()
    expect(within(coder).queryByRole('button', { name: '同步主线' })).toBeNull()

    await user.click(within(coder).getByRole('button', { name: '再交给 Coder' }))
    await waitFor(() =>
      expect(posted).toMatchObject({
        body: '@Coder 你的工作区里合并 main 还没完成：src/api.ts。请解决冲突后提交，然后告诉我。',
        mentions: [{ kind: 'agent', id: 'm1' }],
      }),
    )

    await user.click(within(coder).getByRole('button', { name: '放弃这次合并' }))
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
        return { project: app, rooms: [] }
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

  it('says in a line when members have no worktrees', async () => {
    stub({}, {}, { main: { repo_path: '/src/app', git: false }, members: [] })
    renderView()
    expect(await screen.findByText('项目目录不是 git 仓库，成员都在同一个目录里干活')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '编辑步骤' })).toBeNull()
  })
})
