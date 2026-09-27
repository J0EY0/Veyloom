import { describe, expect, it } from 'vitest'
import type { TranscriptLine } from '@/api/types'
import { drawerLines, withLive } from './lines'

describe('withLive', () => {
  it('follows what was read with what was heard after it', () => {
    const read: TranscriptLine[] = [
      { kind: 'start' },
      { kind: 'event', event: { kind: 'tool_call', tool: 'Read', seq: 1 } },
      { kind: 'event', event: { kind: 'tool_result', tool: 'Read', seq: 2 } },
    ]
    const lines = withLive(read, [
      { kind: 'tool_result', tool: 'Read', seq: 2 },
      { kind: 'text', text: 'ok', at: '2026-09-27T03:00:00Z', seq: 3 },
    ])
    expect(lines.map((line) => line.event?.seq ?? line.kind)).toEqual(['start', 1, 2, 3])
    expect(lines[3]).toEqual({ kind: 'event', at: '2026-09-27T03:00:00Z', event: { kind: 'text', text: 'ok', at: '2026-09-27T03:00:00Z', seq: 3 } })
  })

  it('keeps what was heard when nothing was read, numbered or not', () => {
    expect(
      withLive(
        [],
        [
          { kind: 'status', text: 'thinking' },
          { kind: 'text', text: 'hi', seq: 2 },
        ],
      ),
    ).toHaveLength(2)
  })
})

describe('drawerLines', () => {
  it('joins the words said in pieces, and leaves the session out', () => {
    const lines = drawerLines([
      { kind: 'event', at: '1', event: { kind: 'session', session_ref: 's' } },
      { kind: 'event', at: '2', event: { kind: 'text', text: '我先' } },
      { kind: 'event', at: '3', event: { kind: 'text', text: '看看。' } },
      { kind: 'event', at: '4', event: { kind: 'tool_call', tool: 'Read' } },
      { kind: 'event', at: '5', event: { kind: 'text', text: '好了' } },
    ])
    expect(lines).toEqual([
      { kind: 'event', at: '2', event: { kind: 'text', text: '我先看看。' } },
      { kind: 'event', at: '4', event: { kind: 'tool_call', tool: 'Read' } },
      { kind: 'event', at: '5', event: { kind: 'text', text: '好了' } },
    ])
  })
})
