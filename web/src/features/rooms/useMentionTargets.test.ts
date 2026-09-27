import { createElement, type ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { stubApi } from '@/test/fetch'
import { detectMentions, useMentionTargets, type MentionTarget } from './useMentionTargets'

const targets: MentionTarget[] = [
  { mention: { kind: 'agent', id: 'a1' }, name: 'Codex Implementer' },
  { mention: { kind: 'agent', id: 'a2' }, name: 'Pi Tester' },
  { mention: { kind: 'user', id: 'u1' }, name: 'alice' },
]

describe('detectMentions', () => {
  it('finds every name written with an @, once', () => {
    expect(detectMentions('@Codex Implementer 看看 @alice 的问题 @Codex Implementer', targets)).toEqual([
      { kind: 'agent', id: 'a1' },
      { kind: 'user', id: 'u1' },
    ])
  })

  it('ignores names without an @', () => {
    expect(detectMentions('Pi Tester 说得对', targets)).toEqual([])
  })

  it('takes the longest name after each @', () => {
    const coders: MentionTarget[] = [
      { mention: { kind: 'agent', id: 'c1' }, name: 'Coder' },
      { mention: { kind: 'agent', id: 'c2' }, name: 'Coder2' },
    ]
    expect(detectMentions('@Coder2 看一下', coders)).toEqual([{ kind: 'agent', id: 'c2' }])
    expect(detectMentions('@Coder2 和 @Coder', coders)).toEqual([
      { kind: 'agent', id: 'c1' },
      { kind: 'agent', id: 'c2' },
    ])
    // A name runs on into the words after it.
    expect(detectMentions('@Coder请看', coders)).toEqual([{ kind: 'agent', id: 'c1' }])
  })

  it('leaves a name to its owner though no one can mention them', () => {
    const coder: MentionTarget[] = [{ mention: { kind: 'agent', id: 'c1' }, name: 'Coder' }]
    expect(detectMentions('@Coder2 看一下', coder, ['Coder', 'Coder2'])).toEqual([])
    expect(detectMentions('@Coder2 看一下', coder)).toEqual([{ kind: 'agent', id: 'c1' }])
  })
})

describe('useMentionTargets', () => {
  it('still names an agent taken out of the project, but offers it to nobody', async () => {
    stubApi({
      '/rooms/r1/members': {
        members: [
          { id: 'a1', display_name: 'Codex Implementer', enabled: true },
          { id: 'a2', display_name: 'Pi Tester', enabled: true, removed_at: '2026-09-16T09:00:00Z' },
        ],
      },
      '/users': { users: [] },
    })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const wrapper = ({ children }: { children: ReactNode }) => createElement(QueryClientProvider, { client }, children)
    const { result } = renderHook(() => useMentionTargets('r1'), { wrapper })
    await waitFor(() => expect(result.current.names.get('a2')).toBe('Pi Tester'))
    expect(result.current.members.map((target) => target.name)).toEqual(['Codex Implementer'])
    expect(result.current.all.map((target) => target.name)).toEqual(['Codex Implementer'])
  })
})
