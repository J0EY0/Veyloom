import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { stubApi } from '@/test/fetch'
import { message, project, room } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { ThreadNote } from './ThreadNote'

const held = message('n1', 5, {
  sender_kind: 'system',
  user_id: undefined,
  thread_id: 'th1',
  body: '@alice Ping mentioned Pong, but the last 3 turns agents woke in this piece of work only talked; it waits for a person now.',
})

function stub(continuedAt: string | undefined, onContinue: () => void = () => {}) {
  stubApi({
    '/rooms/r1': { room: room('r1', 'p1', 'main') },
    '/projects': { projects: [project('p1', 'App', '/src/app')] },
    '/threads/th1/relay-holds': {
      holds: [
        {
          message_id: 'n1',
          member_id: 'm2',
          thread_id: 'th1',
          trigger_message_id: 'm4',
          reason: 'idle',
          created_at: '2026-09-24T01:00:00Z',
          continued_at: continuedAt,
        },
      ],
    },
    '/relays/n1/continue': () => {
      onContinue()
      return new Response(null, { status: 202 })
    },
  })
}

// A note telling of a wake a limit held back (docs/design.md 5.22).
describe('a held wake in a topic', () => {
  it("says why in the UI's words, and lets it go on", async () => {
    let went = false
    stub(undefined, () => (went = true))
    renderWithProviders(<ThreadNote message={held} />)
    // The waker's name is brought forward in the line.
    expect(
      await screen.findByText((_, element) => element?.textContent === 'Ping 想唤醒 Pong，但最近 3 轮被唤醒的 agent 都只回复、没有实际操作，等你决定是否继续'),
    ).toBeInTheDocument()
    expect(screen.getByText('Ping').tagName).toBe('B')
    await userEvent.click(await screen.findByRole('button', { name: '继续' }))
    await waitFor(() => expect(went).toBe(true))
  })

  it('says it went on once it has', async () => {
    stub('2026-09-24T01:05:00Z')
    renderWithProviders(<ThreadNote message={held} />)
    expect(await screen.findByText('已继续')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '继续' })).toBeNull()
  })
})
