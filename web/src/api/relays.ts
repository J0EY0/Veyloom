import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { api } from './client'
import { refresh } from './refresh'

// Wakes that a limit on agents waking one another held back (docs/design.md
// 5.22): the note in the topic that told the person of one offers letting
// it go on.

export interface RelayHold {
  // The note that tells of it.
  message_id: string
  member_id: string
  thread_id: string
  trigger_message_id: string
  // idle: the last turns agents woke only talked; limit: the project's
  // relay limit.
  reason: 'idle' | 'limit'
  created_at: string
  continued_at?: string
}

export const relayKeys = {
  all: ['relayHolds'] as const,
  thread: (threadId: string) => ['relayHolds', threadId] as const,
}

// useThreadRelayHolds reads the wakes held back in a topic; only asked for
// where a note tells of one.
export function useThreadRelayHolds(threadId: string, enabled: boolean) {
  return useQuery({
    queryKey: relayKeys.thread(threadId),
    queryFn: async () => (await api.get<{ holds: RelayHold[] }>(`/threads/${threadId}/relay-holds`)).holds,
    enabled: enabled && threadId !== '',
  })
}

// refreshRelayHolds reads a topic's held wakes again: a note came, or one
// went on.
export function refreshRelayHolds(client: QueryClient, threadId: string) {
  refresh(client, { queryKey: relayKeys.thread(threadId) })
}

// useContinueRelay lets a held wake go on, by the note that tells of it.
export function useContinueRelay(threadId: string) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (noteId: string) => api.post<void>(`/relays/${noteId}/continue`, {}),
    onSettled: () => refreshRelayHolds(client, threadId),
  })
}
