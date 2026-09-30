import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { agentKeys } from '@/api/agents'
import { stubApi } from '@/test/fetch'
import { message, summary, user } from '@/test/fixtures'
import { intersectAll } from '@/test/intersection'
import { renderWithProviders } from '@/test/render'
import { continues, Timeline } from './Timeline'

describe('Timeline', () => {
  it('shows the latest page with names and loads older messages at the top', async () => {
    const latest = Array.from({ length: 50 }, (_, i) => message(`m${51 + i}`, 51 + i, { body: `msg ${51 + i}` }))
    const older = [message('m50', 50, { user_id: 'u2', body: 'the older one' })]
    const calls = stubApi({
      '/users': { users: [user('u1', 'alice'), user('u2', 'bob')] },
      '/rooms/r1/members': { members: [] },
      '/rooms/r1/messages': (req) => ({
        messages: new URL(req.url).searchParams.get('before') === '0' ? latest : older,
      }),
    })
    renderWithProviders(<Timeline roomId="r1" />)

    expect(await screen.findByText('msg 100')).toBeInTheDocument()
    expect(await screen.findAllByText('alice')).not.toHaveLength(0)
    expect(screen.queryByText('the older one')).not.toBeInTheDocument()

    intersectAll()
    expect(await screen.findByText('the older one')).toBeInTheDocument()
    expect(screen.getByText('bob')).toBeInTheDocument()
    expect(calls).toContain('GET /rooms/r1/messages?before=51&limit=50')
  })

  it('names agents and renders system notes quietly', async () => {
    stubApi({
      '/users': { users: [] },
      '/rooms/r1/members': { members: [{ id: 'a1', display_name: 'Claude Architect' }] },
      '/rooms/r1/messages': {
        messages: [
          message('m1', 1, { member_id: 'a1', user_id: undefined, body: '收到' }),
          message('m2', 2, { sender_kind: 'system', user_id: undefined, body: 'turn failed' }),
        ],
      },
    })
    renderWithProviders(<Timeline roomId="r1" />)
    expect(await screen.findByText('Claude Architect')).toBeInTheDocument()
    expect(screen.getByText('turn failed')).toBeInTheDocument()
  })

  it("shows an agent's own picture where it speaks and is mentioned, else its runtime's mark", async () => {
    const picture = '0123456789abcdef0123456789abcdef.webp'
    stubApi({
      '/users': { users: [user('u1', 'alice')] },
      '/agents': {
        agents: [
          { id: 'ag1', name: 'Architect', avatar: picture, runtime: 'claude' },
          { id: 'ag2', name: 'Tester', avatar: '', runtime: 'pi' },
        ],
      },
      '/rooms/r1/members': {
        members: [
          { id: 'a1', agent_id: 'ag1', display_name: 'Claude Architect' },
          { id: 'a2', agent_id: 'ag2', display_name: 'Pi Tester' },
        ],
      },
      '/rooms/r1/messages': {
        messages: [
          message('m1', 1, { member_id: 'a1', user_id: undefined, body: '@Pi Tester 你来', mentions: [{ kind: 'agent', id: 'a2' }] }),
          message('m2', 2, { member_id: 'a2', user_id: undefined, body: '好的' }),
          message('m3', 3, { body: '@Claude Architect 谢谢', mentions: [{ kind: 'agent', id: 'a1' }] }),
        ],
      },
    })
    renderWithProviders(<Timeline roomId="r1" />)
    const row = async (text: RegExp) => (await screen.findByText(text)).closest('li') as HTMLElement

    // The architect speaks with its picture; the tester it hands to has
    // none, and its pill shows its name's letter.
    const architect = await row(/你来/)
    expect(architect.querySelector('[data-avatar]')).toHaveAttribute('data-avatar', picture)
    expect(within(within(architect).getByText('Pi Tester')).getByText('P')).toBeInTheDocument()
    // The tester speaks with its letter, Pi's mark in the corner.
    const tester = await row(/好的/)
    expect(tester.querySelector('[data-avatar]')).toBeNull()
    expect(within(tester).getAllByText('P')[0]).toBeInTheDocument()
    expect(tester.querySelector('svg')).not.toBeNull()
    // A person mentioning the architect draws its picture in the pill.
    const thanks = await row(/谢谢/)
    expect(within(thanks).getByText('Claude Architect').querySelector(`[data-avatar="${picture}"]`)).not.toBeNull()
  })

  it('redraws a row drawn already when its agent takes a new picture', async () => {
    const picture = '0123456789abcdef0123456789abcdef.webp'
    let avatar = ''
    stubApi({
      '/users': { users: [] },
      '/agents': () => ({ agents: [{ id: 'ag1', name: 'Architect', avatar, runtime: 'claude' }] }),
      '/rooms/r1/members': { members: [{ id: 'a1', agent_id: 'ag1', display_name: 'Claude Architect' }] },
      '/rooms/r1/messages': { messages: [message('m1', 1, { member_id: 'a1', user_id: undefined, body: '收到' })] },
    })
    const { client } = renderWithProviders(<Timeline roomId="r1" />)
    const row = (await screen.findByText('收到')).closest('li') as HTMLElement
    await waitFor(() => expect(row.querySelector('svg')).not.toBeNull())

    avatar = picture
    await client.invalidateQueries({ queryKey: agentKeys.all })
    await waitFor(() => expect(row.querySelector('[data-avatar]')).toHaveAttribute('data-avatar', picture))
  })

  it("names the leader, lifts the open topic's root, and names the member a root woke", async () => {
    const done = { status: 'done' as const, started_at: '2026-09-25T06:53:51Z', ended_at: '2026-09-25T06:54:37Z' }
    stubApi({
      '/users': { users: [] },
      '/rooms/r1/members': {
        members: [
          { id: 'a1', display_name: 'Lead' },
          { id: 'a2', display_name: 'Coder' },
        ],
      },
      '/rooms/r1/messages': {
        messages: [
          {
            ...message('m1', 1, { member_id: 'a1', user_id: undefined, body: '代码结构清楚了。' }),
            thread: summary('t1', { number: 1, reply_count: 5, turns: 1, last_turn: { id: 'x1', member_id: 'a1', ...done } }),
          },
          {
            ...message('m2', 2, { member_id: 'a1', user_id: undefined, body: '@Coder 请加优先级', created_at: '2026-09-14T02:00:30Z' }),
            thread: summary('t2', { number: 2, reply_count: 2, turns: 1, last_turn: { id: 'x2', member_id: 'a2', ...done } }),
          },
        ],
      },
    })
    renderWithProviders(<Timeline roomId="r1" leaderId="a1" openThreadId="t1" onOpenThread={() => {}} />)
    const first = (await screen.findByText('代码结构清楚了。')).closest('li') as HTMLElement
    expect(within(first).getByText('组长')).toBeInTheDocument()
    expect(within(first).getByRole('button', { name: /#1 · 5 条回复/ })).toBeInTheDocument()
    expect(first.querySelector('.ring-inset')).not.toBeNull()
    // Said a moment later by the leader again: under the first one's face,
    // its topic's status naming the member it woke.
    const second = (await screen.findByText(/请加优先级/)).closest('li') as HTMLElement
    expect(within(second).queryByText('Lead')).toBeNull()
    expect(within(second).getByRole('button', { name: /Coder 已完成 · 46 秒/ })).toBeInTheDocument()
    expect(second.querySelector('.ring-inset')).toBeNull()
  })

  it('shows an empty state', async () => {
    stubApi({ '/users': { users: [] }, '/rooms/r1/members': { members: [] }, '/rooms/r1/messages': { messages: [] } })
    renderWithProviders(<Timeline roomId="r1" />)
    expect(await screen.findByText('还没有消息。')).toBeInTheDocument()
  })

  // "Show in chat" from the attachment viewer (docs/webui.md 4.21).
  it('pages back to the message the address names, brings it into view and lights it', async () => {
    const latest = Array.from({ length: 50 }, (_, i) => message(`m${51 + i}`, 51 + i, { body: `msg ${51 + i}` }))
    const calls = stubApi({
      '/users': { users: [user('u1', 'alice')] },
      '/rooms/r1/members': { members: [] },
      '/rooms/r1/messages': (req) => ({
        messages: new URL(req.url).searchParams.get('before') === '0' ? latest : [message('m50', 50, { body: 'the one asked for' })],
      }),
    })
    const scrolled = vi.spyOn(Element.prototype, 'scrollIntoView')
    const { router } = renderWithProviders(<Timeline roomId="r1" />, { route: '/rooms/r1?message=m50&thread=t9' })
    const row = (await screen.findByText('the one asked for')).closest('li')
    expect(calls).toContain('GET /rooms/r1/messages?before=51&limit=50')
    await waitFor(() => expect(row).toHaveClass('bg-selection'))
    await waitFor(() => expect(scrolled.mock.contexts).toContain(row))
    // Found, the address forgets it and keeps the rest.
    expect(router.state.location.search).toBe('?thread=t9')
    scrolled.mockRestore()
  })

  it('forgets a message it cannot find once there is nothing older', async () => {
    stubApi({
      '/users': { users: [] },
      '/rooms/r1/members': { members: [] },
      '/rooms/r1/messages': { messages: [message('m1', 1, { body: 'the only one' })] },
    })
    const { router } = renderWithProviders(<Timeline roomId="r1" />, { route: '/rooms/r1?message=gone' })
    expect(await screen.findByText('the only one')).toBeInTheDocument()
    await waitFor(() => expect(router.state.location.search).toBe(''))
    expect(document.querySelector('li.bg-selection')).toBeNull()
  })
})

describe('continues', () => {
  const at = (minutes: number) => new Date(Date.UTC(2026, 8, 25, 6, minutes)).toISOString()
  it('holds for the same sender a moment later, and for nothing the system said', () => {
    const lead = (seq: number, minutes: number) => message(`m${seq}`, seq, { member_id: 'a1', user_id: undefined, created_at: at(minutes) })
    expect(continues(undefined, lead(1, 0))).toBe(false)
    expect(continues(lead(1, 0), lead(2, 4))).toBe(true)
    expect(continues(lead(1, 0), lead(2, 6))).toBe(false)
    expect(continues(lead(1, 0), message('m2', 2, { member_id: 'a2', user_id: undefined, created_at: at(1) }))).toBe(false)
    expect(continues(message('m1', 1, { created_at: at(0) }), message('m2', 2, { created_at: at(1) }))).toBe(true)
    expect(continues(lead(1, 0), message('m2', 2, { created_at: at(1) }))).toBe(false)
    const note = (seq: number) => message(`m${seq}`, seq, { sender_kind: 'system', user_id: undefined, created_at: at(0) })
    expect(continues(note(1), note(2))).toBe(false)
  })
})
