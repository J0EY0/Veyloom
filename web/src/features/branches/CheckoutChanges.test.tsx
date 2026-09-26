import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { setCurrentUser } from '@/lib/currentUser'
import { branches, renderView, stub } from './branchesTesting'

// The project's checkout, and merges it stops (docs/design.md 5.21).
describe('the checkout in the branch tab', () => {
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
    await user.click(await screen.findByRole('button', { name: '提交…' }))
    const dialog = await screen.findByRole('dialog', { name: '提交仓库目录的改动' })
    expect(within(dialog).getByRole('checkbox', { name: /notes\.md/ })).toBeChecked()
    await user.click(within(dialog).getByRole('button', { name: '提交' }))
    expect(within(dialog).getByText('提交要写一句说明。')).toBeInTheDocument()
    await user.type(within(dialog).getByLabelText('提交说明'), 'Write the notes down')
    await user.click(within(dialog).getByRole('button', { name: '提交' }))
    await waitFor(() => expect(committed).toEqual({ message: 'Write the notes down', paths: ['notes.md'] }))
  })

  it('says why a merge was refused apart from its message, and what to do', async () => {
    stub({
      '/members/m1/merge': () =>
        new Response(JSON.stringify({ error: 'changes in the way', code: 'checkoutChanged', params: { files: 'notes.md\nsrc/api.ts' } }), {
          status: 409,
          headers: { 'Content-Type': 'application/json' },
        }),
    })
    const user = userEvent.setup()
    renderView()
    const coder = (await screen.findByText('Coder')).closest('[data-slot="item"]') as HTMLElement
    await user.click(within(coder).getByRole('button', { name: '合并到主线' }))
    const dialog = await screen.findByRole('dialog', { name: '把 Coder 的改动合并到 main' })
    await user.click(within(dialog).getByRole('button', { name: '合并到主线' }))
    const alert = await within(dialog).findByRole('alert')
    expect(alert).toHaveTextContent('没能合并')
    expect(alert).toHaveTextContent('仓库目录里的 notes.md、src/api.ts 有没提交的改动，这次合并也要改它们：先提交这些改动，再合并。')
    // The message was not what was wrong: its field is not marked.
    expect(within(dialog).getByLabelText('提交说明').closest('[data-slot="field"]')).not.toHaveAttribute('data-invalid')
    await user.click(within(alert).getByRole('button', { name: '提交仓库目录的改动…' }))
    expect(await screen.findByRole('dialog', { name: '提交仓库目录的改动' })).toBeInTheDocument()
  })

  it("shows each branch's own commits, and whose work it has in full", async () => {
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
    renderView()
    const rows = await screen.findByRole('region', { name: '员工的分支' })
    const second = within(rows).getByText('Tester').closest('[data-slot="item"]') as HTMLElement
    // The latest first.
    expect(
      within(second)
        .getAllByRole('listitem')
        .map((item) => item.textContent),
    ).toEqual(['f36ace9Add comprehensive tests for priority feature', '823b7f5Add priority to todo items'])
    expect(second).toHaveTextContent('已包含 Coder 的改动，先合并哪个都不会冲突')
  })
})
