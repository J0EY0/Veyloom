import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { api } from './client'
import type { Approval, ApprovalResponse, ApprovalsResponse, DecideApprovalRequest, PendingApproval, PendingApprovalsResponse } from './types'

export const approvalKeys = {
  pending: (roomId: string) => ['rooms', roomId, 'approvals', 'pending'] as const,
  all: ['approvals', 'pending'] as const,
  turn: (turnId: string) => ['turns', turnId, 'approvals'] as const,
}

// Everything waiting for a person, across every project, oldest first:
// the inbox and the sidebar count. Polled, since only the open
// chat has a live stream; that chat's own approval events update it too.
export function usePendingApprovalsAll() {
  return useQuery({
    queryKey: approvalKeys.all,
    queryFn: async () => (await api.get<PendingApprovalsResponse>('/approvals?status=pending')).approvals,
    refetchInterval: 10_000,
  })
}

// The room's requests waiting for a person, oldest first.
export function usePendingApprovals(roomId: string) {
  return useQuery({
    queryKey: approvalKeys.pending(roomId),
    queryFn: async () => (await api.get<ApprovalsResponse>(`/rooms/${roomId}/approvals`)).approvals,
    enabled: roomId !== '',
  })
}

// Every request one turn raised, decided or not: what turns the turn's
// system notes into cards.
export function useTurnApprovals(turnId: string) {
  return useQuery({
    queryKey: approvalKeys.turn(turnId),
    queryFn: async () => (await api.get<ApprovalsResponse>(`/turns/${turnId}/approvals`)).approvals,
    enabled: turnId !== '',
  })
}

export function useDecideApproval() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, ...req }: DecideApprovalRequest & { id: string }) => (await api.post<ApprovalResponse>(`/approvals/${id}/decide`, req)).approval,
    onSuccess: (approval) => applyApproval(client, approval),
  })
}

// applyApproval files an approval where the UI reads it: in the room's
// pending list while pending, out of it afterwards, and in its turn's list
// either way. A decision and its event both call it; the second is a no-op.
export function applyApproval(client: QueryClient, approval: Approval) {
  client.setQueryData<Approval[]>(approvalKeys.pending(approval.room_id), (old) => {
    if (!old) return old
    const rest = old.filter((a) => a.id !== approval.id)
    if (approval.status !== 'pending') return rest
    return [...rest, approval].sort((a, b) => a.created_at.localeCompare(b.created_at))
  })
  // The cross-project list learns of a settled request at once; a new one
  // needs the names only the server has, so it is refetched.
  client.setQueryData<PendingApproval[]>(approvalKeys.all, (old) => {
    if (!old) return old
    const rest = old.filter((a) => a.id !== approval.id)
    return approval.status === 'pending' ? old : rest
  })
  if (approval.status === 'pending') void client.invalidateQueries({ queryKey: approvalKeys.all })
  client.setQueryData<Approval[]>(approvalKeys.turn(approval.turn_id), (old) => {
    if (!old) return old
    const index = old.findIndex((a) => a.id === approval.id)
    if (index === -1) return [...old, approval]
    const next = old.slice()
    next[index] = approval
    return next
  })
}
