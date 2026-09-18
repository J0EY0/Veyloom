import { describe, expect, it } from 'vitest'
import { parseTranscript } from './transcript'

describe('parseTranscript', () => {
  it('reads one JSON object per line and skips a torn line', () => {
    const lines = parseTranscript('{"kind":"start","runtime":"fake"}\n{"kind":"event","event":{"kind":"text","text":"hi"}}\n{"kind":"do')
    expect(lines).toEqual([
      { kind: 'start', runtime: 'fake' },
      { kind: 'event', event: { kind: 'text', text: 'hi' } },
    ])
  })
})
