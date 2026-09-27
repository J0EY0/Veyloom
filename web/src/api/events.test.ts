import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { attachment, message, turn } from '@/test/fixtures'
import { attachmentKeys } from './attachments'
import { applyRoomEvent } from './events'
import { threadKeys } from './messages'
import { reminderKeys, type Reminder } from './reminders'
import { draftKeys, type Draft } from './drafts'
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

  // The hub says a running turn went quiet, and that it stirred; a turn
  // read from the store meanwhile keeps what it said (docs/design.md
  // 5.23.8).
  it('files a turn gone quiet, and stirred again, where the running list and the turn read it', () => {
    const client = new QueryClient()
    const running = turn('x1', 't1', { status: 'running' })
    client.setQueryData(turnKeys.running('r1'), [running])
    client.setQueryData(turnKeys.one('x1'), running)
    const since = '2026-09-27T10:00:00Z'
    applyRoomEvent(client, { kind: 'turn_quiet', room_id: 'r1', at: '2026-09-27T10:10:00Z', turn_id: 'x1', quiet_since: since })
    expect(client.getQueryData<Turn[]>(turnKeys.running('r1'))?.[0].quiet_since).toBe(since)
    expect(client.getQueryData<Turn>(turnKeys.one('x1'))?.quiet_since).toBe(since)

    applyRoomEvent(client, { kind: 'turn_trust', room_id: 'r1', at: '2026-09-27T10:11:00Z', turn: { ...running, trusted_by: 'u1' } })
    expect(client.getQueryData<Turn[]>(turnKeys.running('r1'))?.[0]).toMatchObject({ trusted_by: 'u1', quiet_since: since })

    applyRoomEvent(client, { kind: 'turn_quiet', room_id: 'r1', at: '2026-09-27T10:12:00Z', turn_id: 'x1' })
    expect(client.getQueryData<Turn[]>(turnKeys.running('r1'))?.[0].quiet_since).toBeUndefined()
    expect(client.getQueryData<Turn>(turnKeys.one('x1'))?.quiet_since).toBeUndefined()
  })

  // A reminder set, come due or taken back reaches its topic's notes
  // (docs/design.md 5.23.4); a topic not read is left for when it is.
  it("writes a reminder into its topic's list as it stands", () => {
    const client = new QueryClient()
    const set: Reminder = {
      id: 'rm1',
      member_id: 'a1',
      room_id: 'r1',
      thread_id: 't1',
      note: 'check CI',
      due_at: '2026-09-28T01:00:00Z',
      status: 'pending',
      created_at: '2026-09-27T01:00:00Z',
    }
    applyRoomEvent(client, { kind: 'reminder', room_id: 'r1', at: '', reminder: set })
    expect(client.getQueryData(reminderKeys.thread('t1'))).toBeUndefined()
    client.setQueryData<Reminder[]>(reminderKeys.thread('t1'), [])
    applyRoomEvent(client, { kind: 'reminder', room_id: 'r1', at: '', reminder: { ...set, set_message_id: 'n1' } })
    applyRoomEvent(client, { kind: 'reminder', room_id: 'r1', at: '', reminder: { ...set, set_message_id: 'n1', status: 'fired', fired_message_id: 'n2' } })
    expect(client.getQueryData<Reminder[]>(reminderKeys.thread('t1'))).toEqual([{ ...set, set_message_id: 'n1', status: 'fired', fired_message_id: 'n2' }])
  })

  // A draft drafted, run, turned down or replaced reaches its card
  // (docs/design.md 5.23.5).
  it("writes a draft into its topic's list as it stands", () => {
    const client = new QueryClient()
    const drafted: Draft = {
      id: 'd1',
      project_id: 'p1',
      room_id: 'r1',
      thread_id: 't1',
      member_id: 'a1',
      kind: 'merge',
      target_id: 'a2',
      subject: 'work:a2',
      params: { message: 'Add tags' },
      status: 'pending',
      result: {},
      created_at: '2026-09-27T01:00:00Z',
    }
    applyRoomEvent(client, { kind: 'draft', room_id: 'r1', at: '', draft: drafted })
    expect(client.getQueryData(draftKeys.thread('t1'))).toBeUndefined()
    client.setQueryData<Draft[]>(draftKeys.thread('t1'), [drafted])
    const ran: Draft = { ...drafted, status: 'done', result: { commit: 'abc1234' } }
    applyRoomEvent(client, { kind: 'draft', room_id: 'r1', at: '', draft: ran })
    expect(client.getQueryData<Draft[]>(draftKeys.thread('t1'))).toEqual([ran])
  })

  // Files a message brings join the attachments tab and the viewer's walk
  // (docs/webui.md 4.21); a message without any leaves them be.
  it("reads the room's attachments again for a message that brought files", async () => {
    const client = new QueryClient()
    const spy = vi.spyOn(client, 'invalidateQueries')
    const touched = () => spy.mock.calls.map(([filters]) => JSON.stringify(filters?.queryKey))
    applyRoomEvent(client, { kind: 'message', room_id: 'r1', at: '', message: message('m1', 1) })
    await new Promise((resolve) => setTimeout(resolve, 20))
    expect(touched()).not.toContain(JSON.stringify(attachmentKeys.room('r1')))
    applyRoomEvent(client, { kind: 'message', room_id: 'r1', at: '', message: message('m2', 2, { attachments: [attachment('f1', 'shot.png', 'image')] }) })
    await expect.poll(touched).toContain(JSON.stringify(attachmentKeys.room('r1')))
  })
})
