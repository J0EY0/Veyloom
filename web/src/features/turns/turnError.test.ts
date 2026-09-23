import { describe, expect, it } from 'vitest'
import { turnErrorText } from './turnError'

describe('turnErrorText', () => {
  it('words what the hub says of a turn, and leaves a runtime’s words be', () => {
    expect(turnErrorText('machine is offline')).toBe('机器没连上，这一轮没能开始。')
    expect(turnErrorText('machine disconnected')).toBe('机器断开了，这一轮没跑完。')
    expect(turnErrorText('dispatch to machine: connection reset')).toBe('没能把这一轮交给机器：connection reset')
    expect(turnErrorText('codex: turn interrupted')).toBe('codex: turn interrupted')
  })
})
