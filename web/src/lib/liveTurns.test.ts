import { beforeEach, describe, expect, it } from 'vitest'
import { applyTurnEvent, clearLiveTurn, getLiveTurn, resetLiveTurns, seedLiveTurn } from './liveTurns'

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

  it('starts the text anew where the turn was passed a message', () => {
    applyTurnEvent('x9', { kind: 'text', text: '做完了。' })
    applyTurnEvent('x9', { kind: 'steer', text: '>> [alice] 再加一个', steer_id: 's1', seq: 2 })
    expect(getLiveTurn('x9')?.text).toBe('')
    applyTurnEvent('x9', { kind: 'text', text: '好，加上了。', seq: 3 })
    expect(getLiveTurn('x9')?.text).toBe('好，加上了。')
  })

  it('forgets a turn and caps the events it keeps', () => {
    for (let i = 0; i < 2020; i++) applyTurnEvent('x2', { kind: 'status', text: `s${i}` })
    expect(getLiveTurn('x2')?.events).toHaveLength(2000)
    expect(getLiveTurn('x2')?.events[0].text).toBe('s20')
    clearLiveTurn('x2')
    expect(getLiveTurn('x2')).toBeUndefined()
  })

  it('lets the oldest words go before any step', () => {
    applyTurnEvent('x3', { kind: 'tool_call', tool: 'Bash', input: '{"command":"go test"}' })
    for (let i = 0; i < 2000; i++) applyTurnEvent('x3', { kind: 'text', text: `${i} ` })
    const events = getLiveTurn('x3')?.events ?? []
    expect(events).toHaveLength(2000)
    expect(events[0]).toMatchObject({ kind: 'tool_call', tool: 'Bash' })
    expect(events[1].text).toBe('1 ')
  })

  it('takes an event heard twice once, by its number', () => {
    applyTurnEvent('x4', { kind: 'text', text: 'a', seq: 3 })
    applyTurnEvent('x4', { kind: 'text', text: 'a', seq: 3 })
    applyTurnEvent('x4', { kind: 'text', text: 'b', seq: 4 })
    expect(getLiveTurn('x4')).toMatchObject({ text: 'ab', seq: 4 })
  })

  it('lays the transcript read late under what was heard live', () => {
    // The page heard the turn from event 4 on.
    applyTurnEvent('x5', { kind: 'text', text: '好了', seq: 4 })
    applyTurnEvent('x5', { kind: 'tool_call', tool: 'Bash', input: '{"command":"go vet"}', seq: 5 })
    // The transcript was written as far as 4 when read.
    seedLiveTurn('x5', [
      { kind: 'status', text: 'thinking', seq: 1 },
      { kind: 'tool_call', tool: 'Read', input: '{"file_path":"a.go"}', seq: 2 },
      { kind: 'tool_result', tool: 'Read', text: 'package a', seq: 3 },
      { kind: 'text', text: '好了', seq: 4 },
    ])
    const live = getLiveTurn('x5')
    expect(live?.events.map((event) => event.seq)).toEqual([1, 2, 3, 4, 5])
    expect(live).toMatchObject({ tool: 'Bash', text: '', seq: 5 })
    // What comes after is taken in as ever; what came before is not again.
    applyTurnEvent('x5', { kind: 'tool_result', tool: 'Bash', text: 'ok', seq: 6 })
    applyTurnEvent('x5', { kind: 'tool_call', tool: 'Read', input: '{"file_path":"a.go"}', seq: 2 })
    expect(getLiveTurn('x5')?.events.map((event) => event.seq)).toEqual([1, 2, 3, 4, 5, 6])
  })

  it('does not bring back a turn over before its transcript came', () => {
    applyTurnEvent('x6', { kind: 'text', text: 'hi', seq: 1 })
    clearLiveTurn('x6')
    seedLiveTurn('x6', [{ kind: 'text', text: 'hi', seq: 1 }])
    expect(getLiveTurn('x6')).toBeUndefined()
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
