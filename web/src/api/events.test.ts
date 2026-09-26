import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it } from 'vitest'
import { message, turn } from '@/test/fixtures'
import { applyRoomEvent } from './events'
import { threadKeys } from './messages'
import { turnKeys } from './turns'
import type { ThreadResponse, Turn } from './types'

describe('applyRoomEvent', () => {
  // A person letting the rest of a turn's requests through, and taking it
  // back, reaches the open topic and the running list (docs/design.md 4.6).
  it('files a turn trusted and untrusted where the topic and the running list read it', () => {
    const client = new QueryClient()
    const running = turn('x1', 't1', { status: 'running' })
    const open: ThreadResponse = {
      thread: { id: 't1', room_id: 'r1', root_message_id: 'm2', created_at: '2026-09-14T02:00:00Z' },
      root: message('m2', 2, { member_id: 'a1' }),
      turns: [running],
    }
    client.setQueryData(threadKeys.one('t1'), open)
    client.setQueryData(turnKeys.running('r1'), [running])

    const trusted = { ...running, trusted_by: 'u1', trusted_at: '2026-09-14T02:05:00Z' }
    applyRoomEvent(client, { kind: 'turn_trust', room_id: 'r1', at: '2026-09-14T02:05:00Z', turn: trusted })
    expect(client.getQueryData<ThreadResponse>(threadKeys.one('t1'))?.turns[0].trusted_by).toBe('u1')
    expect(client.getQueryData<Turn[]>(turnKeys.running('r1'))?.[0].trusted_by).toBe('u1')

    applyRoomEvent(client, { kind: 'turn_trust', room_id: 'r1', at: '2026-09-14T02:06:00Z', turn: running })
    expect(client.getQueryData<ThreadResponse>(threadKeys.one('t1'))?.turns[0].trusted_by).toBeUndefined()
    expect(client.getQueryData<ThreadResponse>(threadKeys.one('t1'))?.turns).toHaveLength(1)
  })
})
