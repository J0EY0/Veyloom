import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { approval, message } from '@/test/fixtures'
import { FakeWebSocket, stubWebSocket } from '@/test/websocket'
import { approvalKeys } from './approvals'
import { applyInboxEvent, inboxKeys, useInboxEvents } from './inbox'
import type { PendingApproval } from './types'

// The queries a client was asked to read again, by key.
function invalidated(client: QueryClient) {
  const spy = vi.spyOn(client, 'invalidateQueries')
  return () => spy.mock.calls.map(([filters]) => JSON.stringify(filters?.queryKey))
}

// What reaches the person streams in and keeps the inbox current
// (docs/webui.md §5.2).
describe('the inbox stream', () => {
  beforeEach(() => stubWebSocket())

  it('reads the inbox again for a message that reached it, and files requests', async () => {
    const client = new QueryClient()
    const keys = invalidated(client)
    client.setQueryData<PendingApproval[]>(approvalKeys.all, [{ ...approval('ap1'), member_name: 'Careful', project_name: 'App' }])

    applyInboxEvent(client, 'u1', { kind: 'message', room_id: 'r1', at: '', message: message('m1', 1) })
    await expect.poll(keys).toContain(JSON.stringify(inboxKeys.user('u1')))
    applyInboxEvent(client, 'u1', { kind: 'approval_decided', room_id: 'r1', at: '', approval: approval('ap1', { status: 'allowed' }) })
    expect(client.getQueryData(approvalKeys.all)).toEqual([])
  })

  it('opens for the person, reads both again each time it opens, and closes', async () => {
    const client = new QueryClient()
    const keys = invalidated(client)
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
    const { unmount } = renderHook(() => useInboxEvents('u1'), { wrapper })
    const ws = FakeWebSocket.last()
    expect(ws.url).toMatch(/\/api\/v1\/users\/u1\/inbox\/events$/)
    ws.open()
    await expect.poll(keys).toEqual(expect.arrayContaining([JSON.stringify(inboxKeys.user('u1')), JSON.stringify(approvalKeys.all)]))
    unmount()
    expect(ws.closed).toBe(true)

    renderHook(() => useInboxEvents(''), { wrapper })
    expect(FakeWebSocket.instances).toHaveLength(1)
  })
})
