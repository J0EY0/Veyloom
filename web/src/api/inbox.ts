import { useInfiniteQuery } from '@tanstack/react-query'
import { api } from './client'
import type { InboxResponse } from './types'

export const inboxKeys = {
  user: (userId: string) => ['users', userId, 'inbox'] as const,
}

export const INBOX_PAGE = 50

// Everything that mentions the user, newest first, paged backwards by seq.
export function useInbox(userId: string) {
  return useInfiniteQuery({
    queryKey: inboxKeys.user(userId),
    queryFn: async ({ pageParam }) => (await api.get<InboxResponse>(`/users/${userId}/inbox?before=${pageParam}&limit=${INBOX_PAGE}`)).items,
    initialPageParam: 0,
    getNextPageParam: (lastPage) => (lastPage.length === INBOX_PAGE ? lastPage[lastPage.length - 1].seq : undefined),
    enabled: userId !== '',
    staleTime: 30_000,
  })
}
