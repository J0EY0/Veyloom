import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { stubApi } from '@/test/fetch'
import { ProbeTimeout, useProbeMachines } from './agents'
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
