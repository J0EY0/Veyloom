import { describe, expect, it } from 'vitest'
import { message, turn } from '@/test/fixtures'
import { buildParts } from './ThreadTimeline'

const agent = { member_id: 'a1', user_id: undefined }

function shape(parts: ReturnType<typeof buildParts>) {
  return parts.map((part) =>
    part.kind === 'turn'
      ? [[part.turn.id, part.first && 'first', part.last && 'last'].filter(Boolean).join(' '), part.messages.map((m) => m.id)]
      : ['message', [part.message.id]],
  )
}

describe('buildParts', () => {
  it('shows a running turn that has said nothing yet', () => {
    const root = message('m2', 2, { ...agent, body: '' })
    expect(shape(buildParts(root, [], [turn('x1', 't1', { status: 'running', ended_at: undefined })]))).toEqual([['x1 first last', []]])
  })

  it('reads as a chat: what a turn said in a row under one face, people on their own', () => {
    const root = message('m2', 2, { ...agent, body: 'on it', turn_id: 'x1' })
    const replies = [
      message('m3', 3, { ...agent, thread_id: 't1', body: 'done', turn_id: 'x1' }),
      message('m4', 4, { thread_id: 't1', body: 'and now?' }),
      message('m5', 5, { ...agent, thread_id: 't1', body: 'more', turn_id: 'x2' }),
    ]
    const turns = [turn('x1', 't1'), turn('x2', 't1')]
    // The root is the title; opened, it heads its turn again.
    expect(shape(buildParts(root, replies, turns))).toEqual([
      ['x1 first last', ['m3']],
      ['message', ['m4']],
      ['x2 first last', ['m5']],
    ])
    expect(shape(buildParts(root, replies, turns, true))[0]).toEqual(['x1 first last', ['m2', 'm3']])
  })

  it('splits a turn a person wrote into the middle of, and keeps its notes with it', () => {
    const root = message('m2', 2, { ...agent, body: 'looking', turn_id: 'x1' })
    const replies = [
      message('m3', 3, { thread_id: 't1', body: 'also check the docs' }),
      message('m4', 4, { thread_id: 't1', sender_kind: 'system', user_id: undefined, body: 'waiting for approval', turn_id: 'x1' }),
      message('m5', 5, { ...agent, thread_id: 't1', body: 'docs too', turn_id: 'x1' }),
    ]
    expect(shape(buildParts(root, replies, [turn('x1', 't1', { status: 'running', ended_at: undefined })], true))).toEqual([
      ['x1 first', ['m2']],
      ['message', ['m3']],
      ['x1 last', ['m4', 'm5']],
    ])
  })

  it("leaves a person's root to the title and keeps their replies", () => {
    const root = message('m2', 2, { body: 'hello' })
    const replies = [message('m3', 3, { thread_id: 't1', body: 'again' })]
    expect(shape(buildParts(root, replies, []))).toEqual([['message', ['m3']]])
    expect(shape(buildParts(root, replies, [], true))).toEqual([
      ['message', ['m2']],
      ['message', ['m3']],
    ])
  })
})
