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
