import { describe, expect, it } from 'vitest'
import type { Work, WorkTurn } from '@/api/types'
import { at, scaleOf, tookMs, turnLabel, withSeconds } from './work'

const zero = { input_tokens: 0, cache_read_tokens: 0, cache_write_tokens: 0, output_tokens: 0 }

function workTurn(id: string, started_at: string, ended_at: string | undefined, overrides: Partial<WorkTurn> = {}): WorkTurn {
  return {
    id,
    member_id: 'm1',
    thread_id: 't1',
    thread_number: 3,
    status: ended_at ? 'done' : 'running',
    started_at,
    ended_at,
    usage: zero,
    kind: 'task',
    waited_ms: 0,
    files: 0,
    ...overrides,
  }
}

function work(turns: WorkTurn[]): Work {
  return {
    chain: 'c1',
    room_id: 'r1',
    thread_id: 't1',
    thread_number: 3,
    title: 'Add tags',
    ask: 'Add tags',
    asked: [],
    running: turns.some((turn) => !turn.ended_at),
    started_at: turns[0].started_at,
    ended_at: turns.at(-1)?.ended_at,
    turns,
    usage: zero,
    waited_ms: 0,
    events: [],
  }
}

const ms = (iso: string) => Date.parse(iso)

describe('scaleOf', () => {
  it('ticks in seconds for work that took seconds', () => {
    const scale = scaleOf(work([workTurn('a', '2026-09-26T10:00:03Z', '2026-09-26T10:00:08Z')]), 0)
    expect(scale.step).toBe(2000)
    expect(scale.from).toBe(ms('2026-09-26T10:00:02Z'))
    expect(scale.to).toBe(ms('2026-09-26T10:00:08Z'))
    expect(scale.ticks).toHaveLength(4)
    expect(withSeconds(scale)).toBe(true)
  })

  it('ticks in minutes for longer work, out to whole ticks on either side', () => {
    const scale = scaleOf(
      work([workTurn('a', '2026-09-26T10:01:20Z', '2026-09-26T10:05:00Z'), workTurn('b', '2026-09-26T10:06:00Z', '2026-09-26T10:13:40Z')]),
      0,
    )
    expect(scale.step).toBe(2 * 60_000)
    expect(scale.from).toBe(ms('2026-09-26T10:00:00Z'))
    expect(scale.to).toBe(ms('2026-09-26T10:14:00Z'))
    expect(scale.ticks).toHaveLength(8)
    expect(withSeconds(scale)).toBe(false)
  })

  it('runs to now while a turn runs', () => {
    const now = ms('2026-09-26T10:30:00Z')
    const scale = scaleOf(work([workTurn('a', '2026-09-26T10:00:00Z', undefined)]), now)
    expect(scale.to).toBeGreaterThanOrEqual(now)
    expect(at(scale, now)).toBeLessThanOrEqual(100)
  })
})

describe('at', () => {
  it('places a moment as a share of the scale, kept within it', () => {
    const scale = { from: 0, to: 1000, ticks: [0, 1000], step: 1000 }
    expect(at(scale, 250)).toBe(25)
    expect(at(scale, -10)).toBe(0)
    expect(at(scale, 2000)).toBe(100)
  })
})

describe('turnLabel', () => {
  const names = new Map([
    ['m2', 'Coder'],
    ['m3', 'Tester'],
  ])

  it('says what each kind of turn did', () => {
    const turn = (overrides: Partial<WorkTurn>) => workTurn('a', '2026-09-26T10:00:00Z', '2026-09-26T10:01:00Z', overrides)
    expect(turnLabel(turn({ kind: 'task', title: 'Add tags' }), names)).toBe('Add tags')
    expect(turnLabel(turn({ kind: 'task' }), names)).toBe('话题 #3')
    expect(turnLabel(turn({ kind: 'split', woke: ['m2'] }), names)).toBe('拆分任务')
    expect(turnLabel(turn({ kind: 'handoff', woke: ['m2', 'm3', 'gone'] }), names)).toBe('交给 Coder、Tester')
    expect(turnLabel(turn({ kind: 'sumup' }), names)).toBe('汇总答复')
    expect(turnLabel(turn({ kind: 'continue' }), names)).toBe('继续')
    expect(turnLabel(turn({ kind: 'continue', title: 'Fix the test' }), names)).toBe('Fix the test')
  })
})

describe('tookMs', () => {
  it('is how long a turn took, or has so far', () => {
    expect(tookMs(workTurn('a', '2026-09-26T10:00:00Z', '2026-09-26T10:00:42Z'), 0)).toBe(42_000)
    expect(tookMs(workTurn('a', '2026-09-26T10:00:00Z', undefined), ms('2026-09-26T10:01:00Z'))).toBe(60_000)
  })
})
