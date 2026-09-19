import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { summary } from '@/test/fixtures'
import { TopicFooter, topicState } from './TopicFooter'

describe('topicState', () => {
  it('reads the latest turn', () => {
    expect(topicState(summary('t'))).toEqual({ tone: 'idle', text: '话题' })
    expect(topicState(summary('t', { last_turn: { id: 'x', status: 'running', started_at: '' } }))).toEqual({ tone: 'run', text: '正在工作' })
    expect(topicState(summary('t', { last_turn: { id: 'x', status: 'running', started_at: '' } }), 'Bash')).toEqual({ tone: 'run', text: '正在跑 Bash' })
    expect(topicState(summary('t', { last_turn: { id: 'x', status: 'running', started_at: '' } }), 'Bash', { what: 'make test', kind: 'tool_use' })).toEqual({
      tone: 'wait',
      text: '在等你审批 · make test',
    })
    expect(topicState(summary('t', { last_turn: { id: 'x', status: 'failed', error: 'exit 1', started_at: '' } }))).toEqual({
      tone: 'fail',
      text: '失败 · exit 1',
    })
    expect(
      topicState(summary('t', { turns: 2, last_turn: { id: 'x', status: 'done', started_at: '2026-09-14T02:00:00Z', ended_at: '2026-09-14T02:00:38Z' } })),
    ).toEqual({ tone: 'ok', text: '完成 · 2 轮 · 38 秒' })
  })

  it('says so while the runtime compacts the session, unless a person is waited for', () => {
    const running = summary('t', { last_turn: { id: 'x', status: 'running', started_at: '' } })
    expect(topicState(running, undefined, undefined, true)).toEqual({ tone: 'run', text: '整理上下文中' })
    expect(topicState(running, 'Bash', { what: 'make test', kind: 'tool_use' }, true).tone).toBe('wait')
    expect(topicState(running, undefined, { what: 'Which database?', kind: 'question' })).toEqual({ tone: 'wait', text: '在等你回答 · Which database?' })
    expect(topicState(running, undefined, { what: 'deploy “Where to?”', kind: 'form' }).text).toBe('在等你填写 · deploy “Where to?”')
  })
})

describe('TopicFooter', () => {
  it('shows the count and opens the topic', async () => {
    const onOpen = vi.fn()
    render(<TopicFooter summary={summary('t1', { reply_count: 3, last_reply_at: '2026-09-13T01:52:00Z' })} onOpen={onOpen} />)
    expect(screen.getByRole('button')).toHaveTextContent('3 条回复')
    await userEvent.click(screen.getByRole('button'))
    expect(onOpen).toHaveBeenCalledOnce()
  })

  it('names the topic by its number, which is how people and agents refer to it', () => {
    render(<TopicFooter summary={summary('t1', { number: 12, reply_count: 3 })} onOpen={vi.fn()} />)
    expect(screen.getByRole('button')).toHaveTextContent('#12 · 3 条回复')
  })
})
