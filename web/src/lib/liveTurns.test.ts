import { beforeEach, describe, expect, it } from 'vitest'
import { applyTurnEvent, clearLiveTurn, getLiveTurn, resetLiveTurns } from './liveTurns'

describe('liveTurns', () => {
  beforeEach(() => resetLiveTurns())

  it('accumulates text until a tool boundary and tracks the tool', () => {
    applyTurnEvent('x1', { kind: 'text', text: '我先' })
    applyTurnEvent('x1', { kind: 'text', text: '看看。' })
    expect(getLiveTurn('x1')?.text).toBe('我先看看。')
    expect(getLiveTurn('x1')?.tool).toBeUndefined()

    applyTurnEvent('x1', { kind: 'tool_call', tool: 'Bash', input: '{"command":"go test"}' })
    expect(getLiveTurn('x1')).toMatchObject({ text: '', tool: 'Bash' })

    applyTurnEvent('x1', { kind: 'tool_result', tool: 'Bash', text: 'ok' })
    applyTurnEvent('x1', { kind: 'text', text: '好了。' })
    expect(getLiveTurn('x1')?.text).toBe('好了。')
    expect(getLiveTurn('x1')?.tool).toBeUndefined()
    expect(getLiveTurn('x1')?.events).toHaveLength(5)
  })

  it('forgets a turn and caps the events it keeps', () => {
    for (let i = 0; i < 520; i++) applyTurnEvent('x2', { kind: 'status', text: `s${i}` })
    expect(getLiveTurn('x2')?.events).toHaveLength(500)
    expect(getLiveTurn('x2')?.events[0].text).toBe('s20')
    clearLiveTurn('x2')
    expect(getLiveTurn('x2')).toBeUndefined()
  })

  it('knows while the runtime compacts the session', () => {
    applyTurnEvent('t1', { kind: 'compaction', phase: 'start' })
    expect(getLiveTurn('t1')?.compacting).toBe(true)
    applyTurnEvent('t1', { kind: 'compaction', phase: 'end' })
    expect(getLiveTurn('t1')?.compacting).toBe(false)

    // One that failed is over too, and so is one whose end was never
    // heard once the agent speaks again.
    applyTurnEvent('t1', { kind: 'compaction', phase: 'start' })
    applyTurnEvent('t1', { kind: 'compaction', phase: 'failed' })
    expect(getLiveTurn('t1')?.compacting).toBe(false)
    applyTurnEvent('t1', { kind: 'compaction', phase: 'start' })
    applyTurnEvent('t1', { kind: 'text', text: 'back' })
    expect(getLiveTurn('t1')?.compacting).toBe(false)
  })
})
