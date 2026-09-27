import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { stubApi } from '@/test/fetch'
import { approval, turn } from '@/test/fixtures'
import { useMemberStates } from './useMemberStates'

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

describe('useMemberStates', () => {
  it('derives offline, disabled, waiting, working and idle', async () => {
    stubApi({
      '/rooms/r1/members': {
        members: [
          { id: 'a1', display_name: 'A', machine_id: 'w1', enabled: true },
          { id: 'a2', display_name: 'B', machine_id: 'w1', enabled: false },
          { id: 'a3', display_name: 'C', machine_id: 'w1', enabled: true },
          { id: 'a4', display_name: 'D', machine_id: 'gone', enabled: true },
        ],
      },
      '/rooms/r1/turns': {
        turns: [
          turn('x1', 't1', { member_id: 'a1', status: 'running', ended_at: undefined }),
          turn('x3', 't3', { member_id: 'a3', status: 'running', ended_at: undefined }),
        ],
      },
      '/rooms/r1/approvals': { approvals: [approval('ap3', { member_id: 'a3', turn_id: 'x3', thread_id: 't3' })] },
      '/machines': { machines: [{ id: 'w1', name: 'laptop', runtimes: [] }] },
    })
    const { result } = renderHook(() => useMemberStates('r1'), { wrapper })
    await waitFor(() => expect(result.current).toHaveLength(4))
    await waitFor(() => expect(result.current.map((s) => s.status)).toEqual(['working', 'disabled', 'waiting', 'offline']))
    expect(result.current[0].turn?.id).toBe('x1')
    expect(result.current[2].approval?.id).toBe('ap3')
  })

  it('derives paused from a pause of the member, or of its account, which one run out no longer holds', async () => {
    const later = new Date(Date.now() + 3_600_000).toISOString()
    const earlier = new Date(Date.now() - 60_000).toISOString()
    stubApi({
      '/rooms/r1/members': {
        members: [
          { id: 'a1', display_name: 'A', agent_id: 'claude-agent', machine_id: 'w1', enabled: true },
          { id: 'a2', display_name: 'B', agent_id: 'codex-agent', machine_id: 'w1', enabled: true },
          { id: 'a3', display_name: 'C', agent_id: 'codex-agent', machine_id: 'w1', enabled: true },
          { id: 'a4', display_name: 'D', agent_id: 'pi-agent', machine_id: 'w1', enabled: true },
        ],
      },
      '/rooms/r1/turns': { turns: [turn('x3', 't3', { member_id: 'a3', status: 'running', ended_at: undefined })] },
      '/rooms/r1/approvals': { approvals: [] },
      '/machines': { machines: [{ id: 'w1', name: 'laptop', runtimes: [] }] },
      '/agents': {
        agents: [
          { id: 'claude-agent', runtime: 'claude' },
          { id: 'codex-agent', runtime: 'codex' },
          { id: 'pi-agent', runtime: 'pi' },
        ],
      },
      '/pauses': {
        pauses: [
          { id: 'p1', member_id: 'a1', reason: 'failing', detail: '', created_at: earlier },
          { id: 'p2', machine_id: 'w1', runtime: 'codex', reason: 'quota', detail: '', ends_at: later, created_at: earlier },
          { id: 'p3', machine_id: 'w1', runtime: 'pi', reason: 'server', detail: '', ends_at: earlier, created_at: earlier },
        ],
      },
    })
    const { result } = renderHook(() => useMemberStates('r1'), { wrapper })
    // A's own, B's account's; C works through it; D's ran out.
    await waitFor(() => expect(result.current.map((s) => s.status)).toEqual(['paused', 'paused', 'working', 'idle']))
    expect(result.current.map((s) => s.pause?.id)).toEqual(['p1', 'p2', 'p2', undefined])
  })

  it('leaves out members taken out of the project', async () => {
    stubApi({
      '/rooms/r1/members': {
        members: [
          { id: 'a1', display_name: 'A', machine_id: 'w1', enabled: true },
          { id: 'a2', display_name: 'Gone', machine_id: 'w1', enabled: true, removed_at: '2026-09-16T09:00:00Z' },
        ],
      },
      '/rooms/r1/turns': { turns: [] },
      '/rooms/r1/approvals': { approvals: [] },
      '/machines': { machines: [{ id: 'w1', name: 'laptop', runtimes: [] }] },
    })
    const { result } = renderHook(() => useMemberStates('r1'), { wrapper })
    await waitFor(() => expect(result.current.map((s) => s.member.display_name)).toEqual(['A']))
  })
})
