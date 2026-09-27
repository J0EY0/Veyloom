import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { stubApi } from '@/test/fetch'
import { summary, turn } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { TopicFooter, topicState } from './TopicFooter'

describe('topicState', () => {
  it('reads the latest turn', () => {
    expect(topicState(summary('t'))).toMatchObject({ tone: 'idle', text: '话题' })
    expect(topicState(summary('t', { last_turn: { id: 'x', status: 'running', started_at: '' } }))).toMatchObject({ tone: 'run', text: '正在工作' })
    expect(topicState(summary('t', { last_turn: { id: 'x', status: 'running', started_at: '' } }), 'Bash')).toMatchObject({ tone: 'run', text: '正在跑 Bash' })
    expect(
      topicState(summary('t', { last_turn: { id: 'x', status: 'running', started_at: '' } }), 'Bash', { what: 'make test', kind: 'tool_use' }),
    ).toMatchObject({
      tone: 'wait',
      text: '在等你审批 · make test',
    })
    expect(topicState(summary('t', { last_turn: { id: 'x', status: 'failed', error: 'exit 1', started_at: '' } }))).toMatchObject({
      tone: 'fail',
      text: '失败 · exit 1',
    })
    expect(
      topicState(summary('t', { turns: 2, last_turn: { id: 'x', status: 'done', started_at: '2026-09-14T02:00:00Z', ended_at: '2026-09-14T02:00:38Z' } })),
    ).toMatchObject({ tone: 'ok', text: '完成 · 2 轮 · 38 秒' })
    expect(
      topicState(summary('t', { turns: 1, last_turn: { id: 'x', status: 'done', started_at: '2026-09-14T02:00:00Z', ended_at: '2026-09-14T02:00:46Z' } })),
    ).toMatchObject({
      tone: 'ok',
      text: '完成 · 46 秒',
    })
  })

  it('counts the whole piece of work where it began, across its topics', () => {
    // The person's ask: the leader's three turns here, a coder's and a
    // tester's in topics of their own (docs/progress.md, step 154).
    const lead = { id: 'x3', status: 'done' as const, started_at: '2026-09-25T06:56:29Z', ended_at: '2026-09-25T06:56:40Z' }
    const work = { thread_id: 't', chain: 'ask', turns: 5, started_at: '2026-09-25T06:53:26Z', ended_at: '2026-09-25T06:56:40Z', running: false }
    expect(topicState(summary('t', { turns: 3, last_turn: lead, work }))).toMatchObject({ tone: 'ok', text: '完成 · 5 轮 · 3 分 14 秒' })
    // Its own turn over, a member's still running.
    const underWay = { ...work, turns: 4, ended_at: undefined, running: true }
    expect(topicState(summary('t', { turns: 2, last_turn: lead, work: underWay }))).toMatchObject({ tone: 'run', text: '进行中 · 4 轮' })
  })

  it('says so while the runtime compacts the session, unless a person is waited for', () => {
    const running = summary('t', { last_turn: { id: 'x', status: 'running', started_at: '' } })
    expect(topicState(running, undefined, undefined, true)).toMatchObject({ tone: 'run', text: '整理上下文中' })
    expect(topicState(running, 'Bash', { what: 'make test', kind: 'tool_use' }, true).tone).toBe('wait')
    expect(topicState(running, undefined, { what: 'Which database?', kind: 'question' })).toMatchObject({ tone: 'wait', text: '在等你回答 · Which database?' })
    expect(topicState(running, undefined, { what: 'deploy “Where to?”', kind: 'form' }).text).toBe('在等你填写 · deploy “Where to?”')
  })
})

describe('topicState of a quiet turn', () => {
  it('says for how long it showed no sign of life, in the wait colour', () => {
    const running = summary('t', { last_turn: { id: 'x', status: 'running', started_at: '' } })
    expect(topicState(running, 'go test', undefined, false, '12 分钟没有动静')).toMatchObject({ tone: 'wait', text: '正在跑 go test · 12 分钟没有动静' })
    expect(topicState(running, undefined, undefined, true, '12 分钟没有动静')).toMatchObject({ tone: 'wait', text: '整理上下文中 · 12 分钟没有动静' })
    // A person waited for is no one's being stuck.
    expect(topicState(running, undefined, { what: 'make test', kind: 'tool_use' }, false, '12 分钟没有动静').text).toBe('在等你审批 · make test')
  })
})

describe('TopicFooter', () => {
  it('says a running turn went quiet, as the running turns have it', async () => {
    const since = new Date(Date.now() - 12 * 60_000 - 5_000).toISOString()
    stubApi({ '/rooms/r1/turns': { turns: [turn('x1', 't1', { status: 'running', ended_at: undefined, quiet_since: since })] } })
    renderWithProviders(<TopicFooter summary={summary('t1', { last_turn: { id: 'x1', status: 'running', started_at: '' } })} roomId="r1" onOpen={vi.fn()} />)
    expect(await screen.findByText('正在工作 · 12 分钟没有动静')).toBeInTheDocument()
  })

  it('shows the count and opens the topic', async () => {
    const onOpen = vi.fn()
    renderWithProviders(<TopicFooter summary={summary('t1', { reply_count: 3, last_reply_at: '2026-09-13T01:52:00Z' })} onOpen={onOpen} />)
    expect(screen.getByRole('button')).toHaveTextContent('3 条回复')
    await userEvent.click(screen.getByRole('button'))
    expect(onOpen).toHaveBeenCalledOnce()
  })

  it('names the topic by its number, which is how people and agents refer to it', () => {
    renderWithProviders(<TopicFooter summary={summary('t1', { number: 12, reply_count: 3 })} onOpen={vi.fn()} />)
    expect(screen.getByRole('button')).toHaveTextContent('#12 · 3 条回复')
  })

  it('names the member the root woke, whose turns the topic has', () => {
    const done = { id: 'x', status: 'done' as const, started_at: '2026-09-25T06:53:51Z', ended_at: '2026-09-25T06:54:37Z' }
    renderWithProviders(<TopicFooter summary={summary('t2', { number: 2, reply_count: 2, turns: 1, last_turn: done })} worker="Coder" onOpen={vi.fn()} />)
    expect(screen.getByRole('button')).toHaveTextContent('Coder 完成 · 46 秒#2 · 2 条回复')
  })

  it('names no member where the phrase is about several turns or a piece of work', () => {
    const done = { id: 'x', status: 'done' as const, started_at: '2026-09-25T06:53:51Z', ended_at: '2026-09-25T06:54:37Z' }
    const work = { thread_id: 't2', chain: 'ask', turns: 4, started_at: '2026-09-25T06:53:26Z', ended_at: '2026-09-25T06:54:37Z', running: false }
    renderWithProviders(<TopicFooter summary={summary('t2', { number: 2, reply_count: 5, turns: 2, last_turn: done, work })} worker="Coder" onOpen={vi.fn()} />)
    expect(screen.getByRole('button')).toHaveTextContent(/^完成 · 4 轮 · 1 分 11 秒#2/)
  })
})
