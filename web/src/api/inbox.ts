import { pauseKeys } from './pauses'
import { machineKeys } from './agents'
import { useEffect } from 'react'
import { useInfiniteQuery, useMutation, useQueryClient, type InfiniteData, type QueryClient } from '@tanstack/react-query'
import { connectEvents } from '@/lib/roomSocket'
import { applyApproval, approvalKeys } from './approvals'
import { api } from './client'
import { patchQuery } from './live'
import { refresh } from './refresh'
import type { InboxRead, InboxReadResponse, InboxResponse, RoomEvent } from './types'

export const inboxKeys = {
  user: (userId: string) => ['users', userId, 'inbox'] as const,
}

export const INBOX_PAGE = 50

// Everything that mentions the user, newest first, paged backwards by seq;
// each page says how many of all of them are unread.
export function useInbox(userId: string) {
  return useInfiniteQuery({
    queryKey: inboxKeys.user(userId),
    queryFn: ({ pageParam }) => api.get<InboxResponse>(`/users/${userId}/inbox?before=${pageParam}&limit=${INBOX_PAGE}`),
    initialPageParam: 0,
    getNextPageParam: (lastPage) => (lastPage.items.length === INBOX_PAGE ? lastPage.items[lastPage.items.length - 1].seq : undefined),
    enabled: userId !== '',
    staleTime: 30_000,
  })
}

// useMarkInboxRead marks read what it is given of the user's inbox
// (docs/webui.md 4.19): the rows named are read at once, and the count is
// what the hub says is left.
export function useMarkInboxRead(userId: string) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (read: InboxRead) => api.post<InboxReadResponse>(`/users/${userId}/inbox/read`, read),
    onMutate: (read) =>
      markRead(client, userId, (item) => Boolean(read.message_ids?.includes(item.id)) || (read.up_to !== undefined && item.seq <= read.up_to)),
    onSuccess: (res) => {
      patchQuery<InfiniteData<InboxResponse>>(client, inboxKeys.user(userId), (old) =>
        old ? { ...old, pages: old.pages.map((page) => ({ ...page, unread: res.unread })) } : old,
      )
      // A topic's messages are known to the hub only.
      if (res.marked > 0) refresh(client, { queryKey: inboxKeys.user(userId) })
    },
    // What was marked at once was not kept: read the inbox as it is.
    onError: () => refresh(client, { queryKey: inboxKeys.user(userId) }),
  })
}

// markRead marks the rows that match read in the cached inbox.
function markRead(client: QueryClient, userId: string, match: (item: InboxResponse['items'][number]) => boolean) {
  patchQuery<InfiniteData<InboxResponse>>(client, inboxKeys.user(userId), (old) =>
    old
      ? { ...old, pages: old.pages.map((page) => ({ ...page, items: page.items.map((item) => (!item.read && match(item) ? { ...item, read: true } : item)) })) }
      : old,
  )
}

// useInboxEvents keeps the inbox and what waits for a decision current in
// every project, from the stream of what reaches the person (docs/webui.md
// §5.2). Each time the stream opens, both are read again.
export function useInboxEvents(userId: string) {
  const client = useQueryClient()
  useEffect(() => {
    if (userId === '') return
    const socket = connectEvents(`/users/${userId}/inbox/events`, {
      onEvent: (event) => applyInboxEvent(client, userId, event),
      onResync: () => {
        refresh(client, { queryKey: inboxKeys.user(userId) })
        refresh(client, { queryKey: approvalKeys.all })
      },
    })
    return () => socket.close()
  }, [client, userId])
}

// applyInboxEvent files one event of the stream: a message that mentions
// the person has the inbox read again, which names its room and sender; a
// request asked or decided is filed as the room's own events file it.
export function applyInboxEvent(client: QueryClient, userId: string, event: RoomEvent) {
  switch (event.kind) {
    case 'message':
    case 'inbox_read':
      refresh(client, { queryKey: inboxKeys.user(userId) })
      break
    case 'approval_requested':
    case 'approval_decided':
      applyApproval(client, event.approval)
      break
    case 'pause':
      // A pause of an account came with how the account stands, which the
      // machines tell.
      refresh(client, { queryKey: pauseKeys.all })
      refresh(client, { queryKey: machineKeys.all })
      break
    default:
      break
  }
}
