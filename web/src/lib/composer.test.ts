import { describe, expect, it, vi } from 'vitest'
import { insertIntoComposer, queueForComposer, registerComposer } from './composer'

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

  it('keeps text for a composer that is not open yet until it opens', () => {
    queueForComposer('t9', '@Keeper 有疑问：')
    const later = vi.fn()
    const off = registerComposer('t9', later)
    expect(later).toHaveBeenCalledOnce()
    expect(later).toHaveBeenCalledWith('@Keeper 有疑问：')
    // Handed over once; one that is open takes it at once.
    queueForComposer('t9', 'again')
    expect(later).toHaveBeenLastCalledWith('again')
    off()
    const next = vi.fn()
    registerComposer('t9', next)()
    expect(next).not.toHaveBeenCalled()
  })
})
