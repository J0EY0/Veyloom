import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it } from 'vitest'
import { message, summary, turn } from '@/test/fixtures'
import { applyRoomMessage, applyTurn, messageKeys, threadKeys } from './messages'
import type { Message, RoomMessage, ThreadResponse } from './types'

function seed(rows: RoomMessage[]) {
  const client = new QueryClient()
  client.setQueryData(messageKeys.room('r1'), { pages: [rows], pageParams: [0] })
  return client
}

function rows(client: QueryClient): RoomMessage[] {
  return (client.getQueryData(messageKeys.room('r1')) as { pages: RoomMessage[][] }).pages.flat()
}

describe('applyRoomMessage', () => {
  it('appends a new top-level message once', () => {
    const client = seed([message('m1', 1)])
    applyRoomMessage(client, message('m2', 2))
    applyRoomMessage(client, message('m2', 2))
    expect(rows(client).map((m) => m.id)).toEqual(['m1', 'm2'])
  })

  it('fills in a topic root by id and keeps what the summary already knew', () => {
    const root = { ...message('m2', 2, { member_id: 'a1', body: '' }), thread: summary('t1', { turns: 1 }) }
    const client = seed([message('m1', 1), root])
    applyRoomMessage(client, message('m2', 2, { member_id: 'a1', body: 'on it', turn_id: 'x1' }), summary('t1'))
    const [, filled] = rows(client)
    expect(filled.body).toBe('on it')
    expect(filled.turn_id).toBe('x1')
    expect(filled.thread).toEqual(summary('t1', { turns: 1 }))
  })

  it('fills in the root of an open topic too', () => {
    const client = seed([{ ...message('m2', 2, { member_id: 'a1', body: '' }), thread: summary('t1') }])
    const open: ThreadResponse = {
      thread: { id: 't1', room_id: 'r1', root_message_id: 'm2', created_at: '2026-09-14T02:00:00Z' },
      root: message('m2', 2, { member_id: 'a1', body: '' }),
      turns: [],
    }
    client.setQueryData(threadKeys.one('t1'), open)
    client.setQueryData(threadKeys.one('t9'), { ...open, thread: { ...open.thread, id: 't9' }, root: message('m9', 9) })
    applyRoomMessage(client, message('m2', 2, { member_id: 'a1', body: 'on it', turn_id: 'x1' }), summary('t1'))
    expect(client.getQueryData<ThreadResponse>(threadKeys.one('t1'))?.root).toMatchObject({ body: 'on it', turn_id: 'x1' })
    expect(client.getQueryData<ThreadResponse>(threadKeys.one('t9'))?.root.body).toBe('message 9')
  })

  it('counts a reply on its root when the topic is not open', () => {
    const client = seed([{ ...message('m2', 2), thread: summary('t1') }])
    applyRoomMessage(client, message('m3', 3, { thread_id: 't1', created_at: '2026-09-14T03:00:00Z' }))
    expect(rows(client)[0].thread).toMatchObject({ reply_count: 1, last_reply_at: '2026-09-14T03:00:00Z' })
  })

  it('appends a reply to an open topic and counts it once', () => {
    const client = seed([{ ...message('m2', 2), thread: summary('t1') }])
    client.setQueryData<Message[]>(threadKeys.messages('t1'), [])
    const reply = message('m3', 3, { thread_id: 't1' })
    applyRoomMessage(client, reply)
    applyRoomMessage(client, reply)
    expect(client.getQueryData<Message[]>(threadKeys.messages('t1'))).toHaveLength(1)
    expect(rows(client)[0].thread?.reply_count).toBe(1)
  })

  it('leaves the cache alone before the first page is loaded', () => {
    const client = new QueryClient()
    applyRoomMessage(client, message('m1', 1))
    expect(client.getQueryData(messageKeys.room('r1'))).toBeUndefined()
  })
})

describe('applyTurn', () => {
  it('records a turn starting and finishing on the root and in the open topic', () => {
    const client = seed([{ ...message('m2', 2), thread: summary('t1') }])
    client.setQueryData<ThreadResponse>(threadKeys.one('t1'), {
      thread: { id: 't1', room_id: 'r1', root_message_id: 'm2', created_at: '' },
      root: message('m2', 2),
      turns: [],
    })

    applyTurn(client, turn('x1', 't1', { status: 'running', ended_at: undefined }))
    expect(rows(client)[0].thread).toMatchObject({ turns: 1, last_turn: { id: 'x1', status: 'running' } })

    applyTurn(client, turn('x1', 't1', { status: 'done' }))
    expect(rows(client)[0].thread).toMatchObject({ turns: 1, last_turn: { id: 'x1', status: 'done' } })
    expect(client.getQueryData<ThreadResponse>(threadKeys.one('t1'))?.turns).toEqual([turn('x1', 't1', { status: 'done' })])
  })
})
