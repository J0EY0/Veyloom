import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Work, WorkTurn } from '@/api/types'
import { renderWithProviders } from '@/test/render'
import { Waterfall } from './Waterfall'

const zero = { input_tokens: 0, cache_read_tokens: 0, cache_write_tokens: 0, output_tokens: 0 }

function workTurn(id: string, from: string, to: string): WorkTurn {
  return {
    id,
    member_id: 'coder',
    thread_id: 't1',
    thread_number: 3,
    status: 'done',
    started_at: `2026-09-26T10:${from}Z`,
    ended_at: `2026-09-26T10:${to}Z`,
    usage: zero,
    kind: 'task',
    waited_ms: 0,
    files: 0,
  }
}

// Three and a half minutes of work: a tick a minute, 10:00 to 10:04.
const work: Work = {
  chain: 'c1',
  room_id: 'r1',
  thread_id: 't1',
  thread_number: 3,
  title: 'Add tags',
  ask: '',
  asked: [],
  running: false,
  started_at: '2026-09-26T10:00:00Z',
  ended_at: '2026-09-26T10:03:30Z',
  turns: [workTurn('x1', '00:00', '01:00'), workTurn('x2', '01:00', '03:30')],
  usage: zero,
  waited_ms: 0,
  events: [],
}

function labels(container: HTMLElement) {
  return [...container.querySelectorAll<HTMLElement>('[role="columnheader"] span[aria-hidden="true"]')]
}

afterEach(() => vi.restoreAllMocks())

describe('Waterfall', () => {
  it('centres every tick’s label on its tick, the first and last too', () => {
    const { container } = renderWithProviders(<Waterfall work={work} names={new Map([['coder', 'Coder']])} looks={new Map()} now={0} />)
    const all = labels(container)
    expect(all).toHaveLength(5)
    for (const label of all) expect(label).toHaveClass('-translate-x-1/2')
  })

  it('writes every other label where the time column is too narrow for all of them', () => {
    // 8rem for four minutes: 2rem apart, where a label needs about 2.9.
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({ width: 128 } as DOMRect)
    const { container } = renderWithProviders(<Waterfall work={work} names={new Map([['coder', 'Coder']])} looks={new Map()} now={0} />)
    const shown = labels(container)
    expect(shown).toHaveLength(3)
    expect(shown.map((label) => label.style.left)).toEqual(['0%', '50%', '100%'])
  })
})
