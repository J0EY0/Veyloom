import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import type { Branches } from '@/api/types'
import { setCurrentUser } from '@/lib/currentUser'
import { branches, renderView, stub } from './branchesTesting'

// A reviewer's branch: a binary it built, a data file a run of it wrote,
// and a test it wrote, none of them committed.
const withBuilds: Branches = {
  ...branches,
  members: [
    {
      ...branches.members[0],
      status: {
        branch: 'veyloom/coder',
        ahead: 0,
        behind: 0,
        uncommitted: 3,
        files: [
          { path: 'linkkeeper', status: 'A', added: 0, deleted: 0, binary: true, uncommitted: true, new: true },
          { path: 'links.json', status: 'A', added: 1, deleted: 0, uncommitted: true, new: true },
          { path: 'tags_test.go', status: 'A', added: 20, deleted: 0, uncommitted: true, new: true },
        ],
      },
    },
  ],
  overlaps: [],
}

describe('what a merge takes, and a branch reset to the main line', () => {
  beforeEach(() => setCurrentUser({ id: 'u1', name: 'alice' }))
  afterEach(() => setCurrentUser(null))

  it('leaves new files out that a person unticks, a binary from the start', async () => {
    let merged: unknown
    stub({ '/members/m1/merge': async (req: Request) => ((merged = await req.json()), { merge: { commit: 'abcdef1' } }) }, {}, withBuilds)
    const user = userEvent.setup()
    renderView()
    const coder = (await screen.findByText('Coder')).closest('[data-slot="item"]') as HTMLElement
    await user.click(within(coder).getByRole('button', { name: '合并到主线' }))
    const dialog = await screen.findByRole('dialog', { name: '把 Coder 的改动合并到 main' })
    const fresh = within(dialog).getByRole('region', { name: '没提交过的新文件' })
    expect(fresh).toHaveTextContent('不勾的留在 Coder 的工作区里，不进主线')
    expect(within(fresh).getByRole('checkbox', { name: 'linkkeeper' })).not.toBeChecked()
    expect(within(fresh).getByText('二进制')).toBeInTheDocument()
    expect(within(fresh).getByRole('checkbox', { name: 'tags_test.go' })).toBeChecked()
    await user.click(within(fresh).getByRole('checkbox', { name: 'links.json' }))
    await user.click(within(dialog).getByRole('button', { name: '合并到主线' }))
    await waitFor(() => expect(merged).toEqual({ message: 'add the feature', leave: ['linkkeeper', 'links.json'] }))
  })

  it('merges nothing when every file is left out', async () => {
    const onlyBuild: Branches = {
      ...withBuilds,
      members: [{ ...withBuilds.members[0], status: { ...withBuilds.members[0].status!, files: [withBuilds.members[0].status!.files![0]] } }],
    }
    stub({}, {}, onlyBuild)
    const user = userEvent.setup()
    renderView()
    const coder = (await screen.findByText('Coder')).closest('[data-slot="item"]') as HTMLElement
    await user.click(within(coder).getByRole('button', { name: '合并到主线' }))
    const dialog = await screen.findByRole('dialog', { name: '把 Coder 的改动合并到 main' })
    expect(within(dialog).getByRole('button', { name: '合并到主线' })).toBeDisabled()
    await user.click(within(dialog).getByRole('checkbox', { name: 'linkkeeper' }))
    expect(within(dialog).getByRole('button', { name: '合并到主线' })).toBeEnabled()
  })

  it('resets a member’s branch to the main line once a person confirms', async () => {
    let asked = false
    stub({ '/members/m1/set-aside': () => ((asked = true), { ref: 'refs/veyloom/set-aside/veyloom/coder/1' }) }, {}, withBuilds)
    const user = userEvent.setup()
    renderView()
    const coder = (await screen.findByText('Coder')).closest('[data-slot="item"]') as HTMLElement
    await user.click(within(coder).getByRole('button', { name: '更多' }))
    await user.click(await screen.findByRole('menuitem', { name: '重置到主线…' }))
    const confirm = await screen.findByRole('alertdialog', { name: '把 Coder 的分支重置到主线？' })
    expect(confirm).toHaveTextContent('veyloom/coder 会重置到主线（git reset --hard）。3 个文件的改动先归档到 refs/veyloom/set-aside/')
    expect(asked).toBe(false)
    await user.click(within(confirm).getByRole('button', { name: '重置' }))
    await waitFor(() => expect(asked).toBe(true))
    await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull())
  })

  it('does not reset a branch while the member is at work', async () => {
    stub()
    renderView()
    const tester = (await screen.findByText('Tester')).closest('[data-slot="item"]') as HTMLElement
    expect(within(tester).getByRole('button', { name: '更多' })).toBeDisabled()
  })
})
