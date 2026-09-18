import { describe, expect, it } from 'vitest'
import { tokenAt } from './useMentionInput'

describe('tokenAt', () => {
  it('finds the @ word the caret is in', () => {
    expect(tokenAt('hi @Co', 6)).toEqual({ start: 3, query: 'Co' })
    expect(tokenAt('@', 1)).toEqual({ start: 0, query: '' })
  })

  it('stops at whitespace and ignores @ inside words', () => {
    expect(tokenAt('@Codex done', 11)).toBeNull()
    expect(tokenAt('mail me@example.com', 19)).toBeNull()
    expect(tokenAt('no mention', 10)).toBeNull()
  })
})
