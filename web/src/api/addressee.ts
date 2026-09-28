import { queryOptions, useQuery, useQueryClient, type QueryClient, type QueryKey } from '@tanstack/react-query'
import { api } from './client'
import { refresh } from './refresh'
import type { Addressee } from './types'

// Where a person's message that names no member would go, in a room or in
// one of its topics (docs/design.md 4.2). The hub's router answers, the one
// that routes the message, so what the composer says and where the message
// goes never part ways. It changes with the room's members and its leader,
// and in a topic with who runs, talks and speaks there: the member
// mutations and the room's events read it again.
export const addresseeKeys = {
  room: (roomId: string) => ['addressee', roomId] as const,
  // threadId '' is the room itself.
  at: (roomId: string, threadId: string) => ['addressee', roomId, threadId] as const,
}

// addresseeQuery asks where a message that names no member would go, in
// the room or, with threadId, in that topic of it.
export function addresseeQuery(roomId: string, threadId = '') {
  const query = threadId ? `?${new URLSearchParams({ thread_id: threadId })}` : ''
  return queryOptions({
    queryKey: addresseeKeys.at(roomId, threadId),
    queryFn: () => api.get<Addressee>(`/rooms/${roomId}/addressee${query}`),
  })
}

export function useAddressee(roomId: string, threadId = '') {
  return useQuery({ ...addresseeQuery(roomId, threadId), enabled: roomId !== '' })
}

// usePrefetchAddressee asks as the page that holds the composer loads,
// alongside what the page shows, rather than once the composer is there:
// the box opens saying whom a message goes to, not the plain hint first.
export function usePrefetchAddressee(roomId: string, threadId = '') {
  const client = useQueryClient()
  if (roomId !== '' && !client.getQueryState(addresseeKeys.at(roomId, threadId))) {
    client.query(addresseeQuery(roomId, threadId)).catch(() => {})
  }
}

// refreshAddressees reads again the answers under queryKey, something
// having changed them. Those on screen are read again now; the others are
// dropped, to be asked afresh when they show again rather than shown with
// the old answer first.
export function refreshAddressees(client: QueryClient, queryKey: QueryKey) {
  client.removeQueries({ queryKey, type: 'inactive' })
  refresh(client, { queryKey })
}
