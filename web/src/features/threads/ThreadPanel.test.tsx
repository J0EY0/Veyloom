import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { setCurrentUser } from '@/lib/currentUser'
import { stubApi } from '@/test/fetch'
import { approval, message, turn, user } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { ThreadPanel } from './ThreadPanel'

const agent = { member_id: 'a1', user_id: undefined }

function stubTopic(extra: Record<string, unknown> = {}) {
  return stubApi({
    '/users': { users: [user('u1', 'alice')] },
    '/rooms/r1/members': { members: [{ id: 'a1', display_name: 'Codex Implementer' }] },
    '/threads/t1': {
      thread: { id: 't1', room_id: 'r1', root_message_id: 'm2' },
      root: message('m2', 2, { ...agent, body: '好，我来补用例。', turn_id: 'x1' }),
      turns: [turn('x1', 't1', { trigger_message_id: 'm1' })],
    },
    '/threads/t1/messages': { messages: [message('m3', 3, { ...agent, thread_id: 't1', body: '补了两个用例。', turn_id: 'x1' })] },
    '/messages/m1': { message: message('m1', 1, { body: '把掉队的处理补上测试。' }) },
    '/rooms/r1/addressee': { member_id: 'a1', reason: 'talking' },
    ...extra,
  })
}

describe('ThreadPanel', () => {
  beforeEach(() => setCurrentUser({ id: 'u1', name: 'alice' }))

  it('names the topic by what the person asked and reads the rest as a chat', async () => {
    stubTopic()
    const onClose = vi.fn()
    renderWithProviders(<ThreadPanel roomId="r1" roomName="main" threadId="t1" onClose={onClose} />)

    // The person's ask, to where its first sentence ends, is the title; the
    // agent's answer the topic hangs from in the chat opens from it.
    const title = await screen.findByRole('button', { name: '把掉队的处理补上测试' })
    expect(screen.queryByText('好，我来补用例。')).not.toBeInTheDocument()
    await userEvent.click(title)
    expect(screen.getByText('好，我来补用例。')).toBeInTheDocument()
    // No heading per turn: the agent's words under its name.
    expect(screen.queryByText(/第 1 轮/)).not.toBeInTheDocument()
    expect(await screen.findByText('补了两个用例。')).toBeInTheDocument()
    expect(screen.getByText('Codex Implementer')).toBeInTheDocument()
    expect(await screen.findByText('不带 @ 时由 Codex Implementer 回复')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: '关闭话题' }))
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('reads, as it opens, what in the topic is addressed to the person, and nothing when none is', async () => {
    const reads: unknown[] = []
    const addressed = { ...agent, thread_id: 't1', body: '@alice 补了两个用例。', turn_id: 'x1', mentions: [{ kind: 'user' as const, id: 'u1' }] }
    stubTopic({
      '/threads/t1/messages': { messages: [message('m3', 3, addressed)] },
      '/users/u1/inbox/read': async (req: Request) => (reads.push(await req.json()), { marked: 1, unread: 0 }),
    })
    const opened = renderWithProviders(<ThreadPanel roomId="r1" roomName="main" threadId="t1" onClose={vi.fn()} />)
    await waitFor(() => expect(reads).toEqual([{ thread_id: 't1' }]))
    opened.unmount()

    stubTopic({ '/users/u1/inbox/read': async (req: Request) => (reads.push(await req.json()), { marked: 0, unread: 0 }) })
    renderWithProviders(<ThreadPanel roomId="r1" roomName="main" threadId="t1" onClose={vi.fn()} />)
    await screen.findByText('补了两个用例。')
    expect(reads).toHaveLength(1)
  })

  it('names a topic a member was woken into by the task handed to it, less the @', async () => {
    stubTopic({
      '/threads/t1': {
        thread: { id: 't1', room_id: 'r1', number: 7, root_message_id: 'm2' },
        root: message('m2', 2, {
          member_id: 'a2',
          user_id: undefined,
          body: '@Codex Implementer 请审查 tags 功能。重点看过滤',
          mentions: [{ kind: 'agent', id: 'a1' }],
        }),
        turns: [turn('x1', 't1', { trigger_message_id: 'm2' })],
      },
    })
    renderWithProviders(<ThreadPanel roomId="r1" roomName="main" threadId="t1" onClose={vi.fn()} />)
    expect(await screen.findByRole('heading', { name: /^#7\s*请审查 tags 功能$/ })).toBeInTheDocument()
  })

  it('heads the topic with its number', async () => {
    stubTopic({
      '/threads/t1': {
        thread: { id: 't1', room_id: 'r1', number: 12, root_message_id: 'm2' },
        root: message('m2', 2, { ...agent, body: '好，我来补用例。', turn_id: 'x1' }),
        turns: [turn('x1', 't1', { trigger_message_id: 'm1' })],
      },
    })
    renderWithProviders(<ThreadPanel roomId="r1" roomName="main" threadId="t1" onClose={vi.fn()} />)
    // The number and the words are two pieces side by side.
    expect(await screen.findByRole('heading', { name: /^#12\s*好，我来补用例。$/ })).toBeInTheDocument()
  })

  it('shows the whole root when its title opens, the markdown taken off the title', async () => {
    stubTopic({
      '/threads/t1': {
        thread: { id: 't1', room_id: 'r1', root_message_id: 'm2' },
        root: message('m2', 2, { ...agent, body: '**先看路由**，再看协议。\n\n路由在 `internal/hub/router.go`。', turn_id: 'x1' }),
        turns: [turn('x1', 't1', { trigger_message_id: 'm1' })],
      },
    })
    renderWithProviders(<ThreadPanel roomId="r1" roomName="main" threadId="t1" onClose={() => {}} />)

    const title = await screen.findByRole('button', { name: '先看路由，再看协议。 路由在 internal/hub/router.go。' })
    expect(title).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('internal/hub/router.go')).not.toBeInTheDocument()
    await userEvent.click(title)
    expect(title).toHaveAttribute('aria-expanded', 'true')
    // The root heads its turn again, rendered, under the agent's name.
    expect(await screen.findByText('internal/hub/router.go')).toBeInTheDocument()
    expect(screen.getByText('补了两个用例。')).toBeInTheDocument()
    expect(screen.getAllByText('Codex Implementer')).toHaveLength(1)
  })

  it('titles a topic a person opened for two agents with the ask, each agent under its own face', async () => {
    const shot = {
      id: 'f1',
      room_id: 'r1',
      filename: 'shot.png',
      media_type: 'image/png',
      kind: 'image' as const,
      size: 10,
      created_at: '2026-09-14T02:00:00Z',
    }
    const ask = message('m1', 1, {
      body: '一起看看这个。',
      attachments: [shot],
      mentions: [
        { kind: 'agent', id: 'a1' },
        { kind: 'agent', id: 'a2' },
      ],
    })
    stubApi({
      '/users': { users: [user('u1', 'alice')] },
      '/rooms/r1/members': {
        members: [
          { id: 'a1', display_name: 'Codex Implementer' },
          { id: 'a2', display_name: 'Claude Architect' },
        ],
      },
      '/threads/t2': {
        thread: { id: 't2', room_id: 'r1', root_message_id: 'm1' },
        root: ask,
        turns: [turn('x1', 't2', { trigger_message_id: 'm1', member_id: 'a1' }), turn('x2', 't2', { trigger_message_id: 'm1', member_id: 'a2' })],
      },
      '/threads/t2/messages': {
        messages: [
          message('m2', 2, { member_id: 'a1', thread_id: 't2', body: '我先看路由。', turn_id: 'x1' }),
          message('m3', 3, { member_id: 'a2', thread_id: 't2', body: '我看协议。', turn_id: 'x2' }),
        ],
      },
      '/messages/m1': { message: ask },
    })
    renderWithProviders(<ThreadPanel roomId="r1" roomName="main" threadId="t2" onClose={() => {}} />)

    // The ask brought a file, so its title opens to show it.
    const title = await screen.findByRole('button', { name: '一起看看这个' })
    expect(screen.queryByText('一起看看这个。')).not.toBeInTheDocument()
    expect(await screen.findByText('我看协议。')).toBeInTheDocument()
    expect(screen.getByText('Codex Implementer')).toBeInTheDocument()
    expect(screen.getByText('Claude Architect')).toBeInTheDocument()

    expect(screen.queryByRole('button', { name: '预览 shot.png' })).not.toBeInTheDocument()
    await userEvent.click(title)
    expect(screen.getByRole('button', { name: '预览 shot.png' })).toBeInTheDocument()
    expect(screen.getByText('alice')).toBeInTheDocument()
  })

  it('says whom a message without an @ goes to, as the hub answers for the topic', async () => {
    const asked: string[] = []
    stubTopic({
      '/rooms/r1/addressee': (req: Request) => (asked.push(new URL(req.url).search), { member_id: 'a2', reason: 'last' }),
      '/rooms/r1/members': {
        members: [
          { id: 'a1', display_name: 'Codex Implementer' },
          { id: 'a2', display_name: 'Pi Tester' },
        ],
      },
      '/threads/t1': {
        thread: { id: 't1', room_id: 'r1', root_message_id: 'm2' },
        root: message('m2', 2, { ...agent, body: '好，我来补用例。', turn_id: 'x1' }),
        turns: [turn('x1', 't1', { trigger_message_id: 'm1' }), turn('x2', 't1', { trigger_message_id: 'm4', member_id: 'a2' })],
      },
      '/threads/t1/messages': {
        messages: [
          message('m4', 4, { thread_id: 't1', body: '@Pi Tester 你也看看', mentions: [{ kind: 'agent', id: 'a2' }] }),
          message('m5', 5, { member_id: 'a2', user_id: undefined, thread_id: 't1', body: '看过了。', turn_id: 'x2' }),
        ],
      },
    })
    renderWithProviders(<ThreadPanel roomId="r1" roomName="main" threadId="t1" onClose={() => {}} />)

    expect(await screen.findByText('看过了。')).toBeInTheDocument()
    // Pi Tester answers under its own name, after the pill that asked it.
    expect(screen.getAllByText('Pi Tester')).toHaveLength(2)
    expect(await screen.findByText('不带 @ 时由 Pi Tester 回复')).toBeInTheDocument()
    // Asked as the topic opens and, answers going stale at once in tests,
    // again by the box: about the topic each time.
    expect(new Set(asked)).toEqual(new Set(['?thread_id=t1']))
  })

  it('says a message without an @ goes to the leader when the topic has no one else to take it', async () => {
    stubTopic({ '/rooms/r1/addressee': { member_id: 'a1', reason: 'leader_fallback' } })
    renderWithProviders(<ThreadPanel roomId="r1" roomName="main" threadId="t1" onClose={() => {}} />)
    expect(await screen.findByText('不带 @ 时交给组长 Codex Implementer')).toBeInTheDocument()
  })

  it('names a topic whose root has nothing in it yet by the message that started it', async () => {
    stubTopic({
      '/threads/t1': {
        thread: { id: 't1', room_id: 'r1', root_message_id: 'm2' },
        root: message('m2', 2, { ...agent, body: '', turn_id: 'x1' }),
        turns: [turn('x1', 't1', { trigger_message_id: 'm1', status: 'running', ended_at: undefined })],
      },
      '/threads/t1/messages': { messages: [] },
    })
    renderWithProviders(<ThreadPanel roomId="r1" roomName="main" threadId="t1" onClose={() => {}} />)

    expect(await screen.findByRole('heading', { name: '把掉队的处理补上测试' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '把掉队的处理补上测试' })).not.toBeInTheDocument()
    expect(screen.getByText('Codex Implementer')).toBeInTheDocument()
  })

  it('says while the rest of a turn is let through, how many went since when, and takes it back', async () => {
    const calls = stubTopic({
      '/threads/t1': {
        thread: { id: 't1', room_id: 'r1', root_message_id: 'm2' },
        root: message('m2', 2, { ...agent, body: '好，我来补用例。', turn_id: 'x1' }),
        turns: [turn('x1', 't1', { trigger_message_id: 'm1', status: 'running', trusted_by: 'u1', trusted_at: '2026-09-14T02:05:00Z' })],
      },
      '/turns/x1/approvals': {
        approvals: [
          approval('ap1', { turn_id: 'x1', status: 'allowed', decided_by: 'u1', scope: 'turn' }),
          approval('ap2', { turn_id: 'x1', status: 'allowed', decided_by: 'u1', reviewer: 'turn' }),
          approval('ap3', { turn_id: 'x1', status: 'allowed', decided_by: 'u1', reviewer: 'turn' }),
        ],
      },
      '/turns/x1/trust': { turn: turn('x1', 't1', { trigger_message_id: 'm1', status: 'running' }) },
    })
    renderWithProviders(<ThreadPanel roomId="r1" roomName="main" threadId="t1" onClose={vi.fn()} />)
    const band = await screen.findByRole('status', { name: '本轮自动批准' })
    await waitFor(() => expect(band).toHaveTextContent(/起已放行 2 次/))
    await userEvent.click(within(band).getByRole('button', { name: '撤销' }))
    await waitFor(() => expect(calls).toContain('DELETE /turns/x1/trust'))
    await waitFor(() => expect(screen.queryByRole('status', { name: '本轮自动批准' })).toBeNull())
  })

  it('replies inside the topic', async () => {
    let posted: unknown
    stubTopic({
      '/rooms/r1/messages': async (req: Request) => {
        posted = await req.json()
        return Response.json({ message: message('m4', 4, { thread_id: 't1', body: 'ok' }) }, { status: 201 })
      },
    })
    renderWithProviders(<ThreadPanel roomId="r1" roomName="main" threadId="t1" onClose={() => {}} />)
    await screen.findByRole('heading', { name: '好，我来补用例。' })

    await userEvent.type(screen.getByPlaceholderText('回复…'), 'ok{Enter}')
    await waitFor(() => expect(posted).toEqual({ user_id: 'u1', body: 'ok', mentions: [], thread_id: 't1' }))
    expect(await screen.findByText('ok')).toBeInTheDocument()
  })
})
