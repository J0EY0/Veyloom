import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import type { InboxItem, PendingApproval } from '@/api/types'
import { setCurrentUser } from '@/lib/currentUser'
import { stubApi } from '@/test/fetch'
import { approval, message, turn, user } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { stubWebSocket } from '@/test/websocket'
import { InboxPage } from './InboxPage'

const agent = { member_id: 'a1', user_id: undefined }

function mention(id: string, seq: number, overrides: Partial<InboxItem> = {}): InboxItem {
  return {
    ...message(id, seq, { ...agent, mentions: [{ kind: 'user', id: 'u1' }] }),
    room_name: 'main',
    project_name: 'Veyloom',
    sender_name: 'Pi Tester',
    read: false,
    ...overrides,
  }
}

function pending(id: string, overrides: Partial<PendingApproval> = {}): PendingApproval {
  return { ...approval(id), member_name: 'Careful Builder', project_name: 'Veyloom', ...overrides }
}

// The room the picked entries' topics live in.
const room = {
  '/users': { users: [user('u1', 'alice')] },
  '/rooms/r1/members': { members: [{ id: 'a1', display_name: 'Careful Builder' }] },
}

describe('InboxPage', () => {
  beforeEach(() => {
    setCurrentUser({ id: 'u1', name: 'alice' })
    stubWebSocket()
  })

  it('lists what waits for a decision first, then the mentions, each opening in place', async () => {
    stubApi({
      '/approvals': { approvals: [pending('ap1')] },
      '/users/u1/inbox': {
        items: [
          mention('m9', 9, { room_id: 'r2', thread_id: 't5', body: '@alice 看完了，**没问题**', project_name: 'docs-site', sender_name: 'Codex Implementer' }),
          mention('m3', 3, { body: '@alice 一起看看？', sender_name: 'bob' }),
        ],
      },
    })
    renderWithProviders(<InboxPage />, { route: '/inbox', path: '/inbox' })
    const rows = within(await screen.findByRole('list')).getAllByRole('link')
    expect(rows).toHaveLength(3)
    expect(rows[0]).toHaveTextContent('Careful Builder待审批')
    expect(rows[0]).toHaveTextContent('make test')
    expect(rows[0]).toHaveAttribute('href', '/inbox?item=ap1')
    expect(rows[1]).toHaveTextContent('Codex Implementer')
    expect(rows[1]).toHaveTextContent('docs-site')
    // The row reads plain, without the @ that put it here.
    expect(rows[1]).toHaveTextContent('看完了，没问题')
    expect(rows[1]).not.toHaveTextContent('@alice')
    expect(rows[2]).toHaveAttribute('href', '/inbox?item=m3')
    expect(screen.getByText('选择一条查看。')).toBeInTheDocument()
  })

  it('says so with nobody signed in, and when nothing arrived', async () => {
    setCurrentUser(null)
    const first = renderWithProviders(<InboxPage />)
    expect(screen.getByText('正在确认登录状态…')).toBeInTheDocument()
    first.unmount()

    setCurrentUser({ id: 'u1', name: 'alice' })
    stubApi({ '/approvals': { approvals: [] }, '/users/u1/inbox': { items: [] } })
    renderWithProviders(<InboxPage />)
    expect(await screen.findByText('暂无待处理的事项。')).toBeInTheDocument()
    // Nothing to pick, search or narrow: the note alone, no list around it.
    expect(screen.queryByText('选择一条查看。')).not.toBeInTheDocument()
    expect(screen.queryByRole('searchbox')).not.toBeInTheDocument()
    expect(screen.queryByRole('switch')).not.toBeInTheDocument()
  })

  it('narrows to approvals with the switch and by words with the search', async () => {
    stubApi({
      '/approvals': { approvals: [pending('ap1')] },
      '/users/u1/inbox': { items: [mention('m9', 9, { body: '@alice 看完了', project_name: 'docs-site' }), mention('m3', 3, { body: '@alice 构建好了' })] },
    })
    renderWithProviders(<InboxPage />, { route: '/inbox', path: '/inbox' })
    await screen.findByRole('list')
    const only = screen.getByRole('switch', { name: '只看待审批' })

    await userEvent.click(only)
    expect(within(screen.getByRole('list')).getAllByRole('link')).toHaveLength(1)
    await userEvent.click(only)
    expect(within(screen.getByRole('list')).getAllByRole('link')).toHaveLength(3)

    const search = screen.getByRole('searchbox', { name: '搜索' })
    await userEvent.type(search, 'DOCS')
    const [match] = within(screen.getByRole('list')).getAllByRole('link')
    expect(match).toHaveAttribute('href', '/inbox?item=m9')
    await userEvent.type(search, ' 构建')
    expect(await screen.findByText('没有匹配的结果。')).toBeInTheDocument()

    await userEvent.clear(search)
    await userEvent.click(only)
    await userEvent.type(search, 'pi tester')
    expect(await screen.findByText('没有匹配的结果。')).toBeInTheDocument()
  })

  it('opens a request in its topic and decides it there, keeping the topic once it leaves the list', async () => {
    let decided: unknown
    stubApi({
      ...room,
      '/approvals': { approvals: [pending('ap1', { message_id: 'n1' })] },
      '/users/u1/inbox': { items: [] },
      '/threads/t1': {
        thread: { id: 't1', room_id: 'r1', root_message_id: 'm2', created_at: '2026-09-14T02:00:00Z' },
        root: message('m2', 2, { ...agent, body: '', turn_id: 'x1' }),
        turns: [turn('x1', 't1', { status: 'running', ended_at: undefined, trigger_message_id: 'm1' })],
      },
      '/threads/t1/messages': {
        messages: [
          message('n1', 3, { sender_kind: 'system', user_id: undefined, thread_id: 't1', turn_id: 'x1', body: 'Careful Builder wants to run: make test' }),
        ],
      },
      '/messages/m1': { message: message('m1', 1, { body: '@Careful Builder 起个服务做冒烟' }) },
      '/turns/x1/approvals': { approvals: [approval('ap1', { message_id: 'n1' })] },
      '/approvals/ap1/decide': async (req) => {
        decided = await req.json()
        return { approval: approval('ap1', { message_id: 'n1', status: 'allowed', decided_by: 'u1', decided_at: '2026-09-14T02:01:00Z' }) }
      },
    })
    const { router } = renderWithProviders(<InboxPage />, { route: '/inbox?item=ap1', path: '/inbox' })

    const topic = await screen.findByRole('complementary', { name: '话题' })
    // Before the agent says anything, the message that started it names the
    // topic; the agent is at work under its own name.
    expect(await within(topic).findByRole('heading', { name: '起个服务做冒烟' })).toBeInTheDocument()
    expect(await within(topic).findByText('Careful Builder')).toBeInTheDocument()
    expect(within(topic).getByRole('link', { name: '在群聊里打开' })).toHaveAttribute('href', '/rooms/r1?thread=t1')
    expect(within(screen.getByRole('list')).getByRole('link')).toHaveAttribute('aria-current', 'true')

    await userEvent.click(await within(topic).findByRole('button', { name: '允许' }))
    await waitFor(() => expect(decided).toMatchObject({ allow: true }))
    expect(await screen.findByText('暂无待处理的事项。')).toBeInTheDocument()
    expect(screen.getByRole('complementary', { name: '话题' })).toBeInTheDocument()
    expect(router.state.location.search).toBe('?item=ap1')
  })

  it('finds the topic an answer heads through its turn, and Escape puts it away', async () => {
    stubApi({
      ...room,
      '/approvals': { approvals: [] },
      '/users/u1/inbox': { items: [mention('m7', 7, { body: '@alice 做完了', turn_id: 'x2' })] },
      '/turns/x2': { turn: turn('x2', 't2', { trigger_message_id: 'm5' }) },
      '/threads/t2': {
        thread: { id: 't2', room_id: 'r1', root_message_id: 'm6', created_at: '2026-09-14T02:00:00Z' },
        root: message('m6', 6, { ...agent, body: '做完了', turn_id: 'x2' }),
        turns: [turn('x2', 't2', { trigger_message_id: 'm5' })],
      },
      '/threads/t2/messages': { messages: [] },
      '/messages/m5': { message: message('m5', 5, { body: '@Careful Builder 收个尾' }) },
      '/turns/x2/approvals': { approvals: [] },
      '/turns/x2/transcript': () => new Response(''),
    })
    const { router } = renderWithProviders(<InboxPage />, { route: '/inbox', path: '/inbox' })

    await userEvent.click(await screen.findByRole('link', { name: /Pi Tester/ }))
    expect(router.state.location.search).toBe('?item=m7')
    const topic = await screen.findByRole('complementary', { name: '话题' })
    expect(await within(topic).findByRole('link', { name: '在群聊里打开' })).toHaveAttribute('href', '/rooms/r1?thread=t2')
    // Named by what the person asked; the answer it hangs from opens from it.
    expect(await within(topic).findByRole('button', { name: '收个尾' })).toBeInTheDocument()

    await userEvent.keyboard('{Escape}')
    await waitFor(() => expect(router.state.location.search).toBe(''))
    expect(await screen.findByText('选择一条查看。')).toBeInTheDocument()
  })
})
