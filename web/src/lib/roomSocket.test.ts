import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { FakeWebSocket, stubWebSocket } from '@/test/websocket'
import { connectRoomEvents } from './roomSocket'

describe('connectRoomEvents', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    stubWebSocket()
  })
  afterEach(() => vi.useRealTimers())

  it('delivers parsed events and reports status', () => {
    const onEvent = vi.fn()
    const onStatus = vi.fn()
    connectRoomEvents('r1', { onEvent, onResync: vi.fn(), onStatus })
    const ws = FakeWebSocket.last()
    expect(ws.url).toMatch(/\/api\/v1\/rooms\/r1\/events$/)
    expect(onStatus).toHaveBeenLastCalledWith('connecting')

    ws.open()
    expect(onStatus).toHaveBeenLastCalledWith('open')
    ws.frame({ kind: 'turn_started', room_id: 'r1', at: '', turn: { id: 'x1' } })
    ws.raw('not json {')
    expect(onEvent).toHaveBeenCalledOnce()
    expect(onEvent).toHaveBeenCalledWith(expect.objectContaining({ kind: 'turn_started' }))
  })

  it('asks for a resync on opening, since what came before it was missed, and again once back', () => {
    const onResync = vi.fn()
    const onStatus = vi.fn()
    connectRoomEvents('r1', { onEvent: vi.fn(), onResync, onStatus })
    expect(onResync).not.toHaveBeenCalled()
    FakeWebSocket.last().open()
    expect(onResync).toHaveBeenCalledOnce()

    FakeWebSocket.last().drop(1008)
    expect(onStatus).toHaveBeenLastCalledWith('reconnecting')
    expect(FakeWebSocket.instances).toHaveLength(1)
    vi.advanceTimersByTime(1000)
    expect(FakeWebSocket.instances).toHaveLength(2)

    FakeWebSocket.last().drop()
    vi.advanceTimersByTime(1000)
    expect(FakeWebSocket.instances).toHaveLength(2)
    vi.advanceTimersByTime(1000)
    expect(FakeWebSocket.instances).toHaveLength(3)

    FakeWebSocket.last().open()
    expect(onResync).toHaveBeenCalledTimes(2)
    expect(onStatus).toHaveBeenLastCalledWith('open')
  })

  it('stays closed once closed', () => {
    const socket = connectRoomEvents('r1', { onEvent: vi.fn(), onResync: vi.fn() })
    const ws = FakeWebSocket.last()
    ws.open()
    socket.close()
    expect(ws.closed).toBe(true)
    ws.drop()
    vi.advanceTimersByTime(60_000)
    expect(FakeWebSocket.instances).toHaveLength(1)
  })
})
