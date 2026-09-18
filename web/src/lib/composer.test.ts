import { describe, expect, it, vi } from 'vitest'
import { insertIntoComposer, registerComposer } from './composer'

describe('composer channel', () => {
  it('delivers to the target composer, or the room one, or nobody', () => {
    const room = vi.fn()
    const thread = vi.fn()
    const offRoom = registerComposer('room', room)
    const offThread = registerComposer('t1', thread)

    expect(insertIntoComposer('t1', '@A ')).toBe(true)
    expect(thread).toHaveBeenCalledWith('@A ')
    expect(insertIntoComposer('t2', '@B ')).toBe(true)
    expect(room).toHaveBeenCalledWith('@B ')

    offThread()
    offRoom()
    expect(insertIntoComposer('t1', '@C ')).toBe(false)
  })
})
