import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { setCurrentUser } from '@/lib/currentUser'
import { branches, renderView, rowOf, stub } from './branchesTesting'

// The project's checkout, and merges it stops (docs/design.md 5.21).
describe('the checkout in the chat’s info', () => {
  beforeEach(() => setCurrentUser({ id: 'u1', name: 'alice' }))
  afterEach(() => setCurrentUser(null))

  it('commits the files picked in the checkout with the message a person writes', async () => {
    let committed: unknown
    stub({
      '/projects/p1/checkout/commit': async (req: Request) => {
        committed = await req.json()
        return { commit: '1a2b3c4d5e6f' }
      },
    })
    const user = userEvent.setup()
    renderView()
    const checkout = await screen.findByRole('region', { name: '仓库目录中有 1 个文件未提交' })
    await user.click(within(checkout).getByRole('button', { name: '提交…' }))
    const dialog = await screen.findByRole('dialog', { name: '提交仓库目录的改动' })
    expect(within(dialog).getByRole('checkbox', { name: /notes\.md/ })).toBeChecked()
    await user.click(within(dialog).getByRole('button', { name: '提交' }))
    expect(within(dialog).getByText('请填写提交说明。')).toBeInTheDocument()
    await user.type(within(dialog).getByLabelText('提交说明'), 'Write the notes down')
    await user.click(within(dialog).getByRole('button', { name: '提交' }))
    await waitFor(() => expect(committed).toEqual({ message: 'Write the notes down', paths: ['notes.md'] }))
  })

  it('reads what the checkout changed', async () => {
    stub({ '/projects/p1/checkout/diff': { patch: '--- a/notes.md\n+++ b/notes.md\n' } })
    const user = userEvent.setup()
    renderView()
    const checkout = await screen.findByRole('region', { name: '仓库目录中有 1 个文件未提交' })
    await user.click(within(checkout).getByRole('button', { name: '查看改动' }))
    expect(await screen.findByRole('dialog', { name: '仓库目录中未提交的改动' })).toBeInTheDocument()
  })

  it('says why a merge was refused apart from its message, and goes on to commit what is in the way', async () => {
    stub({
      '/members/m1/merge': () =>
        new Response(JSON.stringify({ error: 'changes in the way', code: 'checkoutChanged', params: { files: 'notes.md\nsrc/api.ts' } }), {
          status: 409,
          headers: { 'Content-Type': 'application/json' },
        }),
    })
    const user = userEvent.setup()
    renderView()
    await user.click(await within(await rowOf('Coder')).findByRole('button', { name: '合并' }))
    const dialog = await screen.findByRole('dialog', { name: '把 Coder 的改动合并到 main' })
    await user.click(within(dialog).getByRole('button', { name: '合并到主线' }))
    const alert = await within(dialog).findByRole('alert')
    expect(alert).toHaveTextContent('合并失败')
    expect(alert).toHaveTextContent('仓库目录里的 notes.md、src/api.ts 有没提交的改动，这次合并也要改它们：先提交这些改动，再合并。')
    // The message was not what was wrong: its field is not marked.
    expect(within(dialog).getByLabelText('提交说明').closest('[data-slot="field"]')).not.toHaveAttribute('data-invalid')
    await user.click(within(alert).getByRole('button', { name: '提交仓库目录的改动…' }))
    // One dialog at a time: the merge closes, the commit opens.
    expect(await screen.findByRole('dialog', { name: '提交仓库目录的改动' })).toBeInTheDocument()
    expect(screen.queryByRole('dialog', { name: '把 Coder 的改动合并到 main' })).toBeNull()
  })

  it('says whose work a branch has in full, and drafts the merge from its commits', async () => {
    const [coder, tester] = branches.members
    stub(
      {},
      {},
      {
        ...branches,
        overlaps: [],
        members: [
          { ...coder, status: { ...coder.status!, uncommitted: 0, commits: [{ hash: '823b7f5', subject: 'Add priority to todo items' }] } },
          {
            ...tester,
            busy: false,
            contains: ['m1'],
            draft: 'Add comprehensive tests for priority feature',
            status: {
              ...tester.status!,
              commits: [
                { hash: '823b7f5', subject: 'Add priority to todo items' },
                { hash: 'f36ace9', subject: 'Add comprehensive tests for priority feature' },
              ],
            },
          },
        ],
      },
    )
    const user = userEvent.setup()
    renderView()
    const second = await rowOf('Tester')
    await waitFor(() => expect(second).toHaveTextContent('已包含 Coder 的改动，无论先合并哪个都不会冲突'))
    await user.click(within(second).getByRole('button', { name: '合并' }))
    const dialog = await screen.findByRole('dialog', { name: '把 Tester 的改动合并到 main' })
    expect(within(dialog).getByLabelText('提交说明')).toHaveValue('Add comprehensive tests for priority feature')
    expect(within(dialog).getByText('取自 Tester 的提交说明，可修改')).toBeInTheDocument()
  })
})
