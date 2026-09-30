import { describe, expect, it } from 'vitest'
import { turnErrorText } from './turnError'

describe('turnErrorText', () => {
  it('words what the hub says of a turn, and leaves a runtime’s words be', () => {
    expect(turnErrorText('machine is offline')).toBe('机器未连接，这一轮无法开始。')
    expect(turnErrorText('machine disconnected')).toBe('机器断开连接，这一轮没有完成。')
    expect(turnErrorText('dispatch to machine: connection reset')).toBe('无法把这一轮派发给机器：connection reset')
    expect(turnErrorText('codex: turn interrupted')).toBe('codex: turn interrupted')
  })
})
