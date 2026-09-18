import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it } from 'vitest'
import { approval } from '@/test/fixtures'
import { applyApproval, approvalKeys } from './approvals'
import type { Approval } from './types'

describe('applyApproval', () => {
  it('keeps the pending list ordered and drops what is decided', () => {
    const client = new QueryClient()
    client.setQueryData<Approval[]>(approvalKeys.pending('r1'), [approval('ap2', { created_at: '2026-09-14T02:00:09Z' })])
    client.setQueryData<Approval[]>(approvalKeys.turn('x1'), [])

    applyApproval(client, approval('ap1'))
    applyApproval(client, approval('ap1'))
    expect(client.getQueryData<Approval[]>(approvalKeys.pending('r1'))?.map((a) => a.id)).toEqual(['ap1', 'ap2'])
    expect(client.getQueryData<Approval[]>(approvalKeys.turn('x1'))).toHaveLength(1)

    applyApproval(client, approval('ap1', { status: 'allowed', decided_by: 'u1' }))
    expect(client.getQueryData<Approval[]>(approvalKeys.pending('r1'))?.map((a) => a.id)).toEqual(['ap2'])
    expect(client.getQueryData<Approval[]>(approvalKeys.turn('x1'))?.[0]).toMatchObject({ id: 'ap1', status: 'allowed' })
  })

  it('leaves lists alone before they are loaded', () => {
    const client = new QueryClient()
    applyApproval(client, approval('ap1'))
    expect(client.getQueryData(approvalKeys.pending('r1'))).toBeUndefined()
  })
})
