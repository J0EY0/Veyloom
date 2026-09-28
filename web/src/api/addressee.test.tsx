import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { stubApi } from '@/test/fetch'
import { addresseeKeys, refreshAddressees, useAddressee } from './addressee'
import { useUpdateProject } from './projects'

function setup(routes: Record<string, object>) {
  const calls = stubApi(routes)
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
  const asked = (search = '') => calls.filter((call) => call === `GET /rooms/r1/addressee${search}`).length
  return { client, wrapper, asked }
}

describe('refreshAddressees', () => {
  // What changes an answer reads again the one on screen, and drops the
  // ones that are not, to be asked afresh when they show rather than shown
  // with the old answer first (docs/design.md 4.2).
  it('reads again the answer on screen and drops the others', async () => {
    const { client, wrapper, asked } = setup({ '/rooms/r1/addressee': { member_id: 'a1', reason: 'running' } })
    client.setQueryData(addresseeKeys.at('r1', 't2'), { member_id: 'a2', reason: 'last' })
    const open = renderHook(() => useAddressee('r1', 't1'), { wrapper })
    await waitFor(() => expect(open.result.current.data?.reason).toBe('running'))

    refreshAddressees(client, addresseeKeys.room('r1'))
    await waitFor(() => expect(asked('?thread_id=t1')).toBe(2))
    expect(client.getQueryState(addresseeKeys.at('r1', 't2'))).toBeUndefined()
    expect(asked('?thread_id=t2')).toBe(0)
  })
})

describe('useUpdateProject', () => {
  // Saving a project may choose another leader, and so change whom a
  // message without an @ goes to in its chat.
  it('reads again whom a message without an @ goes to in its chat', async () => {
    const { wrapper, asked } = setup({
      '/projects/p1': { project: { id: 'p1', name: 'P', main_room_id: 'r1' } },
      '/rooms/r1/addressee': { member_id: 'a2', reason: 'leader' },
    })
    const open = renderHook(() => useAddressee('r1'), { wrapper })
    await waitFor(() => expect(open.result.current.data?.member_id).toBe('a2'))
    const { result } = renderHook(() => useUpdateProject('p1'), { wrapper })
    await result.current.mutateAsync({ leader_member_id: 'a2' })
    await waitFor(() => expect(asked()).toBe(2))
  })
})
