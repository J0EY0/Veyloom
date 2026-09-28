import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import type { WorkSummary } from '@/api/types'
import { t } from '@/lib/i18n'
import { ThreadMeta } from './ThreadMeta'
import { workState } from './workState'

const over: WorkSummary = {
  thread_id: 't1',
  chain: 'm1',
  turns: 3,
  started_at: '2026-09-28T02:00:00Z',
  ended_at: '2026-09-28T02:02:51Z',
  running: false,
  last_status: 'done',
  members: ['a1', 'a2'],
}

describe('workState', () => {
  // How a piece of work ended is how its last turn to end did (docs/design.md
  // 5.22): a person stopping it leaves it cancelled, whatever came before.
  it('says how a piece of work stands, and how it ended', () => {
    expect(workState({ ...over, running: true, last_status: 'running', ended_at: undefined }, t)).toEqual({ tone: 'run', text: '进行中 · 3 轮' })
    expect(workState(over, t)).toEqual({ tone: 'ok', text: '完成 · 3 轮 · 2 分 51 秒' })
    expect(workState({ ...over, last_status: 'failed' }, t)).toEqual({ tone: 'fail', text: '失败 · 3 轮 · 2 分 51 秒' })
    expect(workState({ ...over, last_status: 'cancelled' }, t)).toEqual({ tone: 'idle', text: '已取消 · 3 轮 · 2 分 51 秒' })
    expect(workState({ ...over, turns: 1, last_status: 'cancelled' }, t)).toEqual({ tone: 'idle', text: '已取消 · 2 分 51 秒' })
    expect(workState({ ...over, turns: 1, last_status: 'failed' }, t)).toEqual({ tone: 'fail', text: '失败 · 2 分 51 秒' })
    // A summary from before the hub said how it ended reads as done.
    expect(workState({ ...over, last_status: undefined }, t)).toEqual({ tone: 'ok', text: '完成 · 3 轮 · 2 分 51 秒' })
  })
})

describe('ThreadMeta', () => {
  it('heads the topic a piece of work began in with how it ended and who took part', () => {
    const names = new Map([
      ['a1', 'Lead'],
      ['a2', 'Coder'],
    ])
    render(<ThreadMeta threadId="t1" work={{ ...over, last_status: 'cancelled' }} names={names} />)
    expect(screen.getByText('已取消 · 3 轮 · 2 分 51 秒')).toBeInTheDocument()
    expect(screen.getByText('· Lead、Coder')).toBeInTheDocument()
    expect(screen.queryByText(/完成/)).not.toBeInTheDocument()
  })
})
