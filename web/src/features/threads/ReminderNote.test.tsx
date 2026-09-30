import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import type { Reminder } from '@/api/reminders'
import { formatTime } from '@/lib/format'
import { stubApi } from '@/test/fetch'
import { message, project, room } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { ThreadNote } from './ThreadNote'

const due = '2026-09-28T09:00:00+08:00'

const set = message('n1', 5, {
  sender_kind: 'system',
  user_id: undefined,
  thread_id: 'th1',
  body: `Slow set a reminder for ${due}: check how CI went`,
})

function reminder(over: Partial<Reminder> = {}): Reminder {
  return {
    id: 'rm1',
    member_id: 'm2',
    room_id: 'r1',
    thread_id: 'th1',
    note: 'check how CI went',
    due_at: due,
    status: 'pending',
    set_message_id: 'n1',
    created_at: '2026-09-27T08:00:00Z',
    ...over,
  }
}

function stub(r: Reminder, onCancel: () => void = () => {}) {
  stubApi({
    '/rooms/r1': { room: room('r1', 'p1', 'main') },
    '/projects': { projects: [project('p1', 'App', '/src/app')] },
    '/users': { users: [{ id: 'u1', name: 'alice', created_at: '2026-09-01T00:00:00Z' }] },
    '/threads/th1/relay-holds': { holds: [] },
    '/threads/th1/reminders': { reminders: [r] },
    '/reminders/rm1': () => {
      onCancel()
      return Response.json({ reminder: { ...r, status: 'cancelled', cancelled_by: 'u1' } })
    },
  })
}

// A note telling of a member's reminder (docs/design.md 5.23.4).
describe('a reminder in a topic', () => {
  it('says when and what for, and takes it back', async () => {
    let cancelled = false
    stub(reminder(), () => (cancelled = true))
    renderWithProviders(<ThreadNote message={set} />)
    expect(
      await screen.findByText((_, element) => element?.tagName === 'SPAN' && element.textContent === `Slow 设了提醒（${formatTime(due)}）：check how CI went`),
    ).toBeInTheDocument()
    await userEvent.click(await screen.findByRole('button', { name: '取消' }))
    await waitFor(() => expect(cancelled).toBe(true))
    expect(await screen.findByText('alice 取消了')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '取消' })).toBeNull()
  })

  it('says it came due, or that the member took it back, or that it was dropped', async () => {
    for (const [over, said] of [
      [{ status: 'fired', fired_message_id: 'n2' }, '已到点'],
      [{ status: 'cancelled' }, 'Slow 取消了'],
      [{ status: 'dropped' }, '已作废'],
    ] as const) {
      stub(reminder(over))
      const { unmount } = renderWithProviders(<ThreadNote message={set} />)
      expect(await screen.findByText(said)).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: '取消' })).toBeNull()
      unmount()
    }
  })
})
