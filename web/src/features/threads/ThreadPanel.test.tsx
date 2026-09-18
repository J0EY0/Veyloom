import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { setCurrentUser } from '@/lib/currentUser'
import { stubApi } from '@/test/fetch'
import { message, turn, user } from '@/test/fixtures'
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
    ...extra,
  })
}

describe('ThreadPanel', () => {
  beforeEach(() => setCurrentUser({ id: 'u1', name: 'alice' }))

  it('names the topic by the message it hangs from and reads the rest as a chat', async () => {
    stubTopic()
    const onClose = vi.fn()
    renderWithProviders(<ThreadPanel roomId="r1" roomName="main" threadId="t1" onClose={onClose} />)

    // The agent's answer the topic hangs from in the chat is the title, once;
    // it fits, so there is nothing to open. What it answered stays in the chat.
    expect(await screen.findByRole('heading', { name: '好，我来补用例。' })).toBeInTheDocument()
    expect(screen.getAllByText('好，我来补用例。')).toHaveLength(1)
    expect(screen.queryByRole('button', { name: '好，我来补用例。' })).not.toBeInTheDocument()
    expect(screen.queryByText('把掉队的处理补上测试。')).not.toBeInTheDocument()
    // No heading per turn: the agent's words under its name.
    expect(screen.queryByText(/第 1 轮/)).not.toBeInTheDocument()
    expect(await screen.findByText('补了两个用例。')).toBeInTheDocument()
    expect(screen.getByText('Codex Implementer')).toBeInTheDocument()
    expect(screen.getByText('不带 @ 时由 Codex Implementer 回复')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: '关闭话题' }))
    expect(onClose).toHaveBeenCalledOnce()
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
    const shot = { id: 'f1', room_id: 'r1', filename: 'shot.png', media_type: 'image/png', size: 10, created_at: '2026-09-14T02:00:00Z' }
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
    const title = await screen.findByRole('button', { name: '一起看看这个。' })
    expect(screen.getAllByText('一起看看这个。')).toHaveLength(1)
    expect(await screen.findByText('我看协议。')).toBeInTheDocument()
    expect(screen.getByText('Codex Implementer')).toBeInTheDocument()
    expect(screen.getByText('Claude Architect')).toBeInTheDocument()
    // Without an @, the agent that spoke last answers.
    expect(screen.getByText('不带 @ 时由 Claude Architect 回复')).toBeInTheDocument()

    expect(screen.queryByRole('link', { name: '打开附件 shot.png' })).not.toBeInTheDocument()
    await userEvent.click(title)
    expect(screen.getByRole('link', { name: '打开附件 shot.png' })).toBeInTheDocument()
    expect(screen.getByText('alice')).toBeInTheDocument()
  })

  it('hands a message without an @ to the agent that spoke last, not to the one that opened the topic', async () => {
    stubTopic({
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
    expect(screen.getByText('不带 @ 时由 Pi Tester 回复')).toBeInTheDocument()
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

    expect(await screen.findByRole('heading', { name: '把掉队的处理补上测试。' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '把掉队的处理补上测试。' })).not.toBeInTheDocument()
    expect(screen.getByText('Codex Implementer')).toBeInTheDocument()
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
