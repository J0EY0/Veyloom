import { describe, expect, it } from 'vitest'
import { runtimeName, visibleRuntimes } from './runtimes'

describe('runtimeName', () => {
  it('names the product, not the model', () => {
    expect(runtimeName('claude')).toBe('Claude Code')
    expect(runtimeName('codex')).toBe('Codex')
    expect(runtimeName('pi')).toBe('Pi')
  })

  it('shows the id of a runtime it does not know', () => {
    expect(runtimeName('fake')).toBe('fake')
  })
})

describe('visibleRuntimes', () => {
  it('leaves out the test runtime and nothing else', () => {
    const listed = visibleRuntimes([{ name: 'claude' }, { name: 'fake' }, { name: 'gemini' }])
    expect(listed.map((runtime) => runtime.name)).toEqual(['claude', 'gemini'])
  })

  it('treats a machine that has not reported yet as having none', () => {
    expect(visibleRuntimes(null)).toEqual([])
    expect(visibleRuntimes(undefined)).toEqual([])
  })
})
