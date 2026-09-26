import { describe, expect, it } from 'vitest'
import type { Task } from '@/api/types'
import { byMember, byState, countStates, markOf } from './tasks'

const now = Date.parse('2026-09-26T10:00:00Z')

function task(chain: string, overrides: Partial<Task> = {}): Task {
  return {
    chain,
    thread_id: `t-${chain}`,
    thread_number: 1,
    member_id: 'm1',
    title: chain,
    state: 'done',
    started_at: '2026-09-26T09:00:00Z',
    ended_at: '2026-09-26T09:10:00Z',
    turns: 1,
    ...overrides,
  }
}

const chains = (tasks: Task[] | undefined) => (tasks ?? []).map((t) => t.chain)

describe('byState', () => {
  it('puts each task in its column, the latest to move first', () => {
    const columns = byState(
      [
        task('older', { ended_at: '2026-09-26T09:05:00Z' }),
        task('newer', { ended_at: '2026-09-26T09:50:00Z' }),
        task('going', { state: 'running', ended_at: undefined }),
        task('to-merge', { state: 'merge' }),
      ],
      now,
    )
    expect(chains(columns.running)).toEqual(['going'])
    expect(chains(columns.merge)).toEqual(['to-merge'])
    expect(chains(columns.done)).toEqual(['newer', 'older'])
  })

  it('puts the task that began last first among those under way', () => {
    const columns = byState(
      [
        task('first', { state: 'running', ended_at: undefined, started_at: '2026-09-26T09:00:00Z' }),
        task('second', { state: 'running', ended_at: undefined, started_at: '2026-09-26T09:30:00Z' }),
      ],
      now,
    )
    expect(chains(columns.running)).toEqual(['second', 'first'])
  })
})

describe('byMember', () => {
  it('gives each member a column in the order given, under way first', () => {
    const columns = byMember(
      [
        task('a-done', { member_id: 'a' }),
        task('a-merge', { member_id: 'a', state: 'merge' }),
        task('a-going', { member_id: 'a', state: 'running', ended_at: undefined }),
        task('b-done', { member_id: 'b' }),
        task('stranger', { member_id: 'x' }),
      ],
      ['b', 'a', 'c'],
      now,
    )
    expect([...columns.keys()]).toEqual(['b', 'a', 'c'])
    expect(chains(columns.get('a'))).toEqual(['a-going', 'a-merge', 'a-done'])
    expect(chains(columns.get('b'))).toEqual(['b-done'])
    // A member with nothing has an empty column; one not asked for, none.
    expect(columns.get('c')).toEqual([])
    expect(columns.has('x')).toBe(false)
  })
})

describe('countStates', () => {
  it('counts a column by state and leaves out the states it has none of', () => {
    expect(countStates([task('r', { state: 'running' }), task('d1'), task('d2')])).toEqual([
      { state: 'running', n: 1, kind: 'run' },
      { state: 'done', n: 2, kind: 'done' },
    ])
  })

  it('counts the tasks under way as waiting when a request waits on a person', () => {
    expect(countStates([task('r', { state: 'running', waiting: true }), task('m', { state: 'merge' })])).toEqual([
      { state: 'running', n: 1, kind: 'wait' },
      { state: 'merge', n: 1, kind: 'merge' },
    ])
  })
})

describe('markOf', () => {
  it('marks a task by its state, and waiting over it', () => {
    expect(markOf(task('r', { state: 'running' }))).toBe('run')
    expect(markOf(task('m', { state: 'merge' }))).toBe('merge')
    expect(markOf(task('d'))).toBe('done')
    expect(markOf(task('w', { state: 'running', waiting: true }))).toBe('wait')
  })
})
