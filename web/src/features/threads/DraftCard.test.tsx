import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import type { Draft } from '@/api/drafts'
import { app, branches, members } from '@/features/branches/branchesTesting'
import { stubApi } from '@/test/fetch'
import { message, room, user } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { ThreadNote } from './ThreadNote'

const card = message('n1', 5, {
  sender_kind: 'system',
  user_id: undefined,
  thread_id: 'th1',
  body: "Lead drafted putting Coder's work on the main line, as: Add the feature\n\nIt does what was asked.",
})

function draft(over: Partial<Draft> = {}): Draft {
  return {
    id: 'd1',
    project_id: 'p1',
    room_id: 'r1',
    thread_id: 'th1',
    member_id: 'm0',
    kind: 'merge',
    target_id: 'm1',
    subject: 'work:m1',
    params: { message: 'Add the feature\n\nIt does what was asked.' },
    then: 'hand the tests to Tester',
    status: 'pending',
    result: {},
    message_id: 'n1',
    created_at: '2026-09-27T08:00:00Z',
    ...over,
  }
}

// The hub's answers, the draft as it stands after a run or a no.
function stub(d: Draft, onRun: (body: unknown) => void = () => {}) {
  let now = d
  stubApi({
    '/rooms/r1': { room: room('r1', 'p1', 'main') },
    '/projects': { projects: [app] },
    '/rooms/r1/members': { members },
    '/users': { users: [user('u1', 'alice')] },
    '/projects/p1/branches': { branches },
    '/threads/th1/relay-holds': { holds: [] },
    '/threads/th1/reminders': { reminders: [] },
    '/threads/th1/drafts': () => Response.json({ drafts: [now] }),
    '/drafts/d1/run': async (req: Request) => {
      onRun(await req.json())
      now = { ...d, status: 'done', decided_by: 'u1', result: { commit: 'abc1234def' } }
      return Response.json({ draft: now })
    },
    '/drafts/d1/decline': () => {
      now = { ...d, status: 'declined', decided_by: 'u1' }
      return Response.json({ draft: now })
    },
  })
}

// The card of what a member drafted for a person (docs/design.md 5.23.5).
describe('a draft in a topic', () => {
  it('says what it does, and runs with one press', async () => {
    let ran: unknown
    stub(draft(), (body) => (ran = body))
    renderWithProviders(<ThreadNote message={card} />)
    expect(await screen.findByText((_, el) => el?.tagName === 'SPAN' && el.textContent === 'Lead 起草：把 Coder 的活合进主线')).toBeInTheDocument()
    expect(await screen.findByText((_, el) => el?.tagName === 'PRE' && el.textContent === 'Add the feature\n\nIt does what was asked.')).toBeInTheDocument()
    expect(await screen.findByText('做完后 Lead 接着：hand the tests to Tester')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '合并' }))
    await waitFor(() => expect(ran).toEqual({ message: '', leave: [] }))
    expect(await screen.findByText('alice 合并了 · abc1234')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '合并' })).toBeNull()
  })

  it('is merged as a person changed it, from the merge dialog', async () => {
    let ran: unknown
    stub(draft(), (body) => (ran = body))
    renderWithProviders(<ThreadNote message={card} />)
    await userEvent.click(await screen.findByRole('button', { name: '改一下…' }))
    const dialog = await screen.findByRole('dialog')
    const text = within(dialog).getByLabelText('提交说明')
    expect(text).toHaveValue('Add the feature\n\nIt does what was asked.')
    expect(within(dialog).getByText('取自 Lead 起草的说明，可以改')).toBeInTheDocument()
    await userEvent.clear(text)
    await userEvent.type(text, 'Add the feature, polished')
    await userEvent.click(within(dialog).getByRole('button', { name: '合并到主线' }))
    await waitFor(() => expect(ran).toMatchObject({ message: 'Add the feature, polished' }))
  })

  it('is turned down', async () => {
    stub(draft())
    renderWithProviders(<ThreadNote message={card} />)
    await userEvent.click(await screen.findByRole('button', { name: '不用' }))
    expect(await screen.findByText('alice 没用')).toBeInTheDocument()
  })

  it('says what came of it once settled: conflicts handed on, or drafted anew', async () => {
    stub(draft({ status: 'conflicted', decided_by: 'u1', result: { conflicts: ['README.md', 'src/api.ts'] } }))
    const { unmount } = renderWithProviders(<ThreadNote message={card} />)
    expect(await screen.findByText('有冲突，没合进去')).toBeInTheDocument()
    expect(screen.getByText('README.md')).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: '交给 Coder 解决' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '合并' })).toBeNull()
    unmount()

    stub(draft({ status: 'superseded' }))
    renderWithProviders(<ThreadNote message={card} />)
    expect(await screen.findByText('有了新的')).toBeInTheDocument()
  })

  it('names what the other kinds do', async () => {
    const aside = message('n2', 6, {
      sender_kind: 'system',
      user_id: undefined,
      thread_id: 'th1',
      body: "Lead drafted giving Coder's work up, since: wrong approach",
    })
    stub(draft({ id: 'd1', kind: 'set_aside', params: { reason: 'wrong approach' }, then: undefined, message_id: 'n2' }))
    renderWithProviders(<ThreadNote message={aside} />)
    expect(await screen.findByText('wrong approach')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '放弃这些活' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '改一下…' })).toBeNull()
  })
})

// The leader's setup steps waiting for a person are a card like any draft
// (docs/design.md 5.21, 5.23.5).
describe('setup steps in a topic', () => {
  const steps = message('n4', 7, {
    sender_kind: 'system',
    user_id: undefined,
    thread_id: 'th1',
    body: 'Lead wrote down how a new worktree is got ready: copy .env, then run npm ci. A person adopts the command before it runs.',
  })

  it('are adopted from the card, which says so', async () => {
    stub(
      draft({
        kind: 'setup_steps',
        target_id: undefined,
        subject: 'setup',
        params: { steps: { copy: ['.env'], run: 'npm ci' } },
        then: undefined,
        message_id: 'n4',
      }),
    )
    renderWithProviders(<ThreadNote message={steps} />)
    expect(await screen.findByText('npm ci')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '采用' }))
    expect(await screen.findByText('alice 采用了')).toBeInTheDocument()
  })
})
