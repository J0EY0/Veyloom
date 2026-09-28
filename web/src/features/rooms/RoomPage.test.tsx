import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { stubApi } from '@/test/fetch'
import { approval, message, room, summary, turn, user } from '@/test/fixtures'
import { lastChat } from '@/lib/lastChat'
import { renderWithProviders } from '@/test/render'
import { FakeWebSocket, stubWebSocket } from '@/test/websocket'
import { addresseeKeys } from '@/api/addressee'
import { RoomPage } from './RoomPage'

const agent = { member_id: 'a1', user_id: undefined }

// stubRoom serves room r1; later are messages said after the first two.
function stubRoom(members: object[] = [{ id: 'a1', display_name: 'Codex Implementer' }], later: object[] = [], addressee: object = { reason: 'none' }) {
  stubApi({
    '/rooms/r1': { room: room('r1', 'p1', 'main') },
    '/users': { users: [user('u1', 'alice')] },
    '/rooms/r1/members': { members },
    '/rooms/r1/messages': {
      messages: [
        message('m1', 1, { body: '@Codex Implementer 补测试' }),
        {
          ...message('m2', 2, { ...agent, body: '好。', turn_id: 'x1' }),
          thread: summary('t1', {
            turns: 1,
            reply_count: 1,
            last_turn: { id: 'x1', status: 'done', started_at: '2026-09-14T02:00:00Z', ended_at: '2026-09-14T02:00:42Z' },
          }),
        },
        ...later,
      ],
    },
    '/threads/t1': {
      thread: { id: 't1', room_id: 'r1', root_message_id: 'm2' },
      root: message('m2', 2, { ...agent, body: '好。', turn_id: 'x1' }),
      turns: [turn('x1', 't1', { trigger_message_id: 'm1' })],
    },
    '/threads/t1/messages': { messages: [] },
    '/messages/m1': { message: message('m1', 1, { body: '@Codex Implementer 补测试' }) },
    '/rooms/r1/approvals': { approvals: [] },
    '/rooms/r1/turns': { turns: [] },
    '/machines': { machines: [] },
    '/rooms/r1/addressee': addressee,
  })
}

describe('RoomPage', () => {
  beforeEach(() => stubWebSocket())

  it('opens a topic from its footer and closes it back to the room', async () => {
    stubRoom()
    const { router } = renderWithProviders(<RoomPage />, { route: '/rooms/r1', path: '/rooms/:roomId' })
    expect(await screen.findByText('好。')).toBeInTheDocument()
    expect(screen.queryByRole('complementary', { name: '话题' })).not.toBeInTheDocument()
    // Opening Veyloom comes back to this chat.
    expect(lastChat()).toBe('r1')

    await userEvent.click(screen.getByRole('button', { name: /完成/ }))
    expect(router.state.location.search).toBe('?thread=t1')
    expect(await screen.findByRole('complementary', { name: '话题' })).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: '关闭话题' }))
    expect(router.state.location.search).toBe('')
    await waitFor(() => expect(screen.queryByRole('complementary', { name: '话题' })).not.toBeInTheDocument())
  })

  it('reads the room again once its stream opens: what was said while the page loaded shows', async () => {
    stubRoom()
    renderWithProviders(<RoomPage />, { route: '/rooms/r1', path: '/rooms/:roomId' })
    await screen.findByText('好。')
    // Said before the stream listened: no event will bring it.
    stubRoom(undefined, [message('m3', 3, { body: '部署好了吗？' })])
    FakeWebSocket.last().open()
    expect(await screen.findByText('部署好了吗？')).toBeInTheDocument()
  })

  it('shows live messages and turn state from the socket', async () => {
    stubRoom()
    const { client } = renderWithProviders(<RoomPage />, { route: '/rooms/r1', path: '/rooms/:roomId' })
    await screen.findByText('好。')
    const ws = FakeWebSocket.last()
    ws.open()
    // Opening reads the room again, for what came before the stream
    // listened; the live events follow.
    await waitFor(() => expect(client.isFetching()).toBe(0))

    ws.frame({ kind: 'message', room_id: 'r1', at: '', message: message('m5', 5, { ...agent, body: '' }), thread: summary('t2') })
    ws.frame({ kind: 'turn_started', room_id: 'r1', at: '', turn: turn('x2', 't2', { status: 'running', ended_at: undefined }) })
    expect(await screen.findByText('正在输入…')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /正在工作/ })).toBeInTheDocument()

    ws.frame({ kind: 'message', room_id: 'r1', at: '', message: message('m5', 5, { ...agent, body: '收到。', turn_id: 'x2' }), thread: summary('t2') })
    expect(await screen.findByText('收到。')).toBeInTheDocument()
    expect(screen.queryByText('正在输入…')).not.toBeInTheDocument()
  })
})

describe('RoomPage approvals', () => {
  beforeEach(() => stubWebSocket())

  it('opens the pending panel from the URL and counts in the title', async () => {
    stubRoom()
    const { client } = renderWithProviders(<RoomPage />, { route: '/rooms/r1?panel=approvals', path: '/rooms/:roomId' })
    expect(await screen.findByRole('complementary', { name: '待审批' })).toBeInTheDocument()
    expect(await screen.findByText('没有在等你的。')).toBeInTheDocument()

    const ws = FakeWebSocket.last()
    ws.open()
    await waitFor(() => expect(client.isFetching()).toBe(0))
    ws.frame({ kind: 'approval_requested', room_id: 'r1', at: '', approval: approval('ap1', { thread_id: 't1' }) })
    expect(await screen.findByText('make test')).toBeInTheDocument()
    await waitFor(() => expect(document.title).toBe('(1) main · Veyloom'))
  })
})

describe('RoomPage side panels', () => {
  beforeEach(() => stubWebSocket())
  afterEach(() => vi.restoreAllMocks())

  const chat = () => document.querySelector<HTMLElement>('[data-room-chat]')!

  it('pushes a wide enough chat aside, only as far as it must, and leaves it open on a press in the chat', async () => {
    // A 75rem chat area (jsdom measures nothing on its own).
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 0, 1200, 800))
    stubRoom()
    const { router } = renderWithProviders(<RoomPage />, { route: '/rooms/r1', path: '/rooms/:roomId' })
    const reply = await screen.findByText('好。')

    await userEvent.click(screen.getByRole('button', { name: '群聊信息' }))
    expect(await screen.findByRole('complementary', { name: '群聊信息' })).toBeInTheDocument()
    // The 53.75rem column needs 17.75rem more to clear the 19.5rem panel.
    expect(chat().style.paddingRight).toBe('17.75rem')
    await userEvent.click(reply)
    expect(router.state.location.search).toBe('?panel=members')

    // A topic is wider: the chat gives up all the room it takes.
    await userEvent.click(screen.getByRole('button', { name: /完成/ }))
    expect(await screen.findByRole('complementary', { name: '话题' })).toBeInTheDocument()
    expect(chat().style.paddingRight).toBe('26.25rem')
  })

  it('lays a panel over a narrow chat and closes it on a press outside', async () => {
    stubRoom()
    const { router } = renderWithProviders(<RoomPage />, { route: '/rooms/r1', path: '/rooms/:roomId' })
    const reply = await screen.findByText('好。')
    const toggle = screen.getByRole('button', { name: '群聊信息' })

    await userEvent.click(toggle)
    expect(router.state.location.search).toBe('?panel=members')
    const panel = await screen.findByRole('complementary', { name: '群聊信息' })
    expect(toggle).toHaveAttribute('aria-pressed', 'true')
    // Nothing makes room for it: the chat keeps its width.
    expect(chat().style.paddingRight).toBe('')

    // A press inside leaves it open, and so does one that only closes a
    // menu it opened.
    await userEvent.click(within(panel).getByRole('heading', { name: '成员' }))
    await userEvent.click(within(panel).getByRole('button', { name: '更多' }))
    expect(await screen.findByRole('menu')).toBeInTheDocument()
    // An open menu leaves the page unclickable (pointer-events: none) until it goes.
    await userEvent.click(reply, { pointerEventsCheck: 0 })
    await waitFor(() => expect(screen.queryByRole('menu')).not.toBeInTheDocument())
    expect(router.state.location.search).toBe('?panel=members')

    // Its button closes it again; a press on the chat does too.
    await userEvent.click(toggle)
    expect(router.state.location.search).toBe('')
    await userEvent.click(toggle)
    expect(await screen.findByRole('complementary', { name: '群聊信息' })).toBeInTheDocument()
    await userEvent.click(reply)
    await waitFor(() => expect(router.state.location.search).toBe(''))

    // A topic footer opens its topic rather than counting as outside.
    await userEvent.click(screen.getByRole('button', { name: /完成/ }))
    expect(router.state.location.search).toBe('?thread=t1')
    expect(await screen.findByRole('complementary', { name: '话题' })).toBeInTheDocument()
    await userEvent.click(reply)
    await waitFor(() => expect(router.state.location.search).toBe(''))
  })
})

describe('RoomPage escape', () => {
  beforeEach(() => stubWebSocket())

  it('closes the open topic with Escape', async () => {
    stubRoom()
    const { router } = renderWithProviders(<RoomPage />, { route: '/rooms/r1?thread=t1', path: '/rooms/:roomId' })
    expect(await screen.findByRole('complementary', { name: '话题' })).toBeInTheDocument()
    await userEvent.keyboard('{Escape}')
    await waitFor(() => expect(router.state.location.search).toBe(''))
  })
  it('says whom a message without an @ goes to, as the hub answers for the room', async () => {
    const codex = { id: 'a1', display_name: 'Codex Implementer', enabled: true }
    const pi = { id: 'a2', display_name: 'Pi Tester', enabled: true }
    for (const [members, addressee, hint] of [
      // With one member the chat is a conversation with it.
      [[codex], { member_id: 'a1', reason: 'only' }, '不带 @ 时由 Codex Implementer 回复'],
      // With more, the leader takes it or hands it on.
      [[codex, pi], { member_id: 'a2', reason: 'leader' }, '不带 @ 时交给组长 Pi Tester 分派'],
      // Nobody would take it: the box asks for an @.
      [[{ ...codex, enabled: false }], { reason: 'none' }, '输入 @ 提到成员'],
    ] as const) {
      stubRoom([...members], [], addressee)
      const view = renderWithProviders(<RoomPage />, { route: '/rooms/r1', path: '/rooms/:roomId' })
      // Once the hub has answered: nobody's answer reads as the box's plain
      // hint, which it shows while it asks too.
      await waitFor(() => expect(view.client.getQueryState(addresseeKeys.at('r1', ''))?.status).toBe('success'))
      expect(await screen.findByText(hint)).toBeInTheDocument()
      expect(screen.queryAllByText(/不带 @ 时/)).toHaveLength(addressee.reason === 'none' ? 0 : 1)
      view.unmount()
    }
  })
})
