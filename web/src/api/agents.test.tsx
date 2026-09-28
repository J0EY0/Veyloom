import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { stubApi } from '@/test/fetch'
import { addresseeKeys, useAddressee } from './addressee'
import { ProbeTimeout, useProbeMachines, useUpdateMember } from './agents'
import type { Machine } from './types'

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

function machine(id: string, probedAt = '2026-09-15T09:13:00Z'): Machine {
  return { id, name: id, runtimes: [], connected_at: '2026-09-15T09:13:00Z', last_seen: '2026-09-15T09:13:00Z', probed_at: probedAt }
}

describe('useProbeMachines', () => {
  it('names the machines that did not answer in time', async () => {
    stubApi({
      '/machines': { machines: [machine('laptop'), machine('build', '2026-09-16T10:00:00Z')] },
      '/machines/laptop/probe': new Response(null, { status: 202 }),
      '/machines/build/probe': new Response(null, { status: 202 }),
    })
    const { result } = renderHook(() => useProbeMachines({ intervalMs: 5, timeoutMs: 30 }), { wrapper })
    const error = await result.current.mutateAsync([machine('laptop'), machine('build')]).catch((err: unknown) => err)
    expect(error).toBeInstanceOf(ProbeTimeout)
    expect((error as ProbeTimeout).names).toEqual(['laptop'])
  })

  it('does not wait for a machine that went away', async () => {
    stubApi({
      '/machines': { machines: [machine('laptop', '2026-09-16T10:00:00Z')] },
      '/machines/laptop/probe': new Response(null, { status: 202 }),
      '/machines/gone/probe': Response.json({ error: 'machine is not connected' }, { status: 404 }),
    })
    const { result } = renderHook(() => useProbeMachines({ intervalMs: 5, timeoutMs: 1000 }), { wrapper })
    const fresh = await result.current.mutateAsync([machine('laptop'), machine('gone')])
    expect(fresh.map((machine) => machine.id)).toEqual(['laptop'])
  })
})

describe('useUpdateMember', () => {
  // A member switched on or off may change who leads, and whom a message
  // without an @ goes to in its room and the room's topics (docs/design.md
  // 4.2): the answer on screen is read again, the others are dropped to be
  // asked afresh when they show; other rooms keep theirs.
  it('reads again whom a message without an @ goes to, in the room and its topics', async () => {
    const calls = stubApi({
      '/members/a1': { member: { id: 'a1', room_id: 'r1', display_name: 'Coder', enabled: false } },
      '/rooms/r1/addressee': { member_id: 'a1', reason: 'talking' },
    })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const withClient = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
    client.setQueryData(addresseeKeys.at('r1', ''), { member_id: 'a1', reason: 'leader' })
    client.setQueryData(addresseeKeys.at('r2', ''), { member_id: 'a9', reason: 'only' })
    const open = renderHook(() => useAddressee('r1', 't1'), { wrapper: withClient })
    await waitFor(() => expect(open.result.current.data?.reason).toBe('talking'))
    const { result } = renderHook(() => useUpdateMember('r1'), { wrapper: withClient })
    await result.current.mutateAsync({ id: 'a1', patch: { enabled: false } })
    await waitFor(() => expect(calls.filter((call) => call === 'GET /rooms/r1/addressee?thread_id=t1')).toHaveLength(2))
    expect(client.getQueryState(addresseeKeys.at('r1', ''))).toBeUndefined()
    expect(client.getQueryState(addresseeKeys.at('r2', ''))?.isInvalidated).toBe(false)
  })
})
