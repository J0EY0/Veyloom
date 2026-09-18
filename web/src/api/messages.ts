import { useInfiniteQuery, useMutation, useQuery, useQueryClient, type InfiniteData, type QueryClient } from '@tanstack/react-query'
import { api } from './client'
import type {
  Message,
  MessageResponse,
  MessagesResponse,
  PostMessageRequest,
  RoomMessage,
  RoomMessagesResponse,
  ThreadResponse,
  ThreadSummary,
  Turn,
} from './types'

export const PAGE_SIZE = 50

export const messageKeys = {
  room: (roomId: string) => ['rooms', roomId, 'messages'] as const,
  one: (messageId: string) => ['messages', messageId] as const,
}

export const threadKeys = {
  one: (threadId: string) => ['threads', threadId] as const,
  messages: (threadId: string) => ['threads', threadId, 'messages'] as const,
}

type RoomPages = InfiniteData<RoomMessage[], number>

// The timeline is an infinite query whose pages are prepended as the
// reader scrolls up: pages[0] is the oldest page loaded, and the page
// param is the seq to load before (0 means the latest).
export function useRoomMessages(roomId: string) {
  return useInfiniteQuery({
    queryKey: messageKeys.room(roomId),
    queryFn: async ({ pageParam }) => (await api.get<RoomMessagesResponse>(`/rooms/${roomId}/messages?before=${pageParam}&limit=${PAGE_SIZE}`)).messages,
    initialPageParam: 0,
    getPreviousPageParam: (firstPage) => (firstPage.length === PAGE_SIZE ? firstPage[0].seq : undefined),
    getNextPageParam: () => undefined,
    enabled: roomId !== '',
  })
}

export function useMessage(messageId: string) {
  return useQuery({
    queryKey: messageKeys.one(messageId),
    queryFn: async () => (await api.get<MessageResponse>(`/messages/${messageId}`)).message,
    enabled: messageId !== '',
  })
}

export function useThread(threadId: string) {
  return useQuery({
    queryKey: threadKeys.one(threadId),
    queryFn: () => api.get<ThreadResponse>(`/threads/${threadId}`),
    enabled: threadId !== '',
  })
}

// A topic's replies. One page: a topic is one piece of work, and the
// server caps a page at 200.
export function useThreadMessages(threadId: string) {
  return useQuery({
    queryKey: threadKeys.messages(threadId),
    queryFn: async () => (await api.get<MessagesResponse>(`/threads/${threadId}/messages?limit=200`)).messages,
    enabled: threadId !== '',
  })
}

// applyRoomMessage puts a message where the room shows it: a top-level
// message at the end of the timeline (or over an earlier copy of itself,
// which is how a topic root gets its text), a reply at the end of its
// topic with the root's summary bumped. A successful post and the
// WebSocket `message` event both call it, so duplicates by id are ignored
// and the two can race.
export function applyRoomMessage(client: QueryClient, message: Message, thread?: ThreadSummary) {
  if (!message.thread_id) {
    // A root getting its text says which topic it heads; an open topic
    // shows the root too, so it gets the text as well.
    if (thread) {
      client.setQueryData<ThreadResponse>(threadKeys.one(thread.id), (old) =>
        old && old.root.id === message.id ? { ...old, root: { ...old.root, ...message } } : old,
      )
    }
    updateRoom(client, message.room_id, (pages) => {
      const found = locate(pages, (m) => m.id === message.id)
      if (found) {
        const existing = pages[found.page][found.index]
        const next: RoomMessage = { ...existing, ...message, thread: mergeSummary(existing.thread, thread) }
        return replaceAt(pages, found, next)
      }
      const last = pages.length - 1
      const next = pages.slice()
      next[last] = [...pages[last], { ...message, thread: mergeSummary(undefined, thread) }]
      return next
    })
    return
  }

  const threadId = message.thread_id
  let appended = false
  client.setQueryData<Message[]>(threadKeys.messages(threadId), (old) => {
    if (!old) return old
    const index = old.findIndex((m) => m.id === message.id)
    if (index !== -1) {
      // The same id again is a rewrite: an approval note settled.
      const next = old.slice()
      next[index] = message
      return next
    }
    appended = true
    return [...old, message]
  })
  updateRoom(client, message.room_id, (pages) => {
    const found = locate(pages, (m) => m.thread?.id === threadId)
    if (!found) return pages
    const root = pages[found.page][found.index]
    const summary = root.thread as ThreadSummary
    // Only count replies we could not see already: when the topic is open
    // the list above is the source of truth for the count.
    const wasLoaded = client.getQueryData<Message[]>(threadKeys.messages(threadId)) !== undefined
    if (wasLoaded && !appended) return pages
    return replaceAt(pages, found, {
      ...root,
      thread: { ...summary, reply_count: summary.reply_count + 1, last_reply_at: message.created_at },
    })
  })
}

// applyTurn records a turn starting or finishing on its topic's summary
// and, if the topic is open, in its turn list.
export function applyTurn(client: QueryClient, turn: Turn) {
  updateRoom(client, turn.room_id, (pages) => {
    const found = locate(pages, (m) => m.thread?.id === turn.thread_id)
    if (!found) return pages
    const root = pages[found.page][found.index]
    const summary = root.thread as ThreadSummary
    const isNew = summary.last_turn?.id !== turn.id
    return replaceAt(pages, found, {
      ...root,
      thread: {
        ...summary,
        turns: isNew ? summary.turns + 1 : summary.turns,
        last_turn: {
          id: turn.id,
          status: turn.status,
          error: turn.error,
          started_at: turn.started_at,
          ended_at: turn.ended_at,
        },
      },
    })
  })
  client.setQueryData<ThreadResponse>(threadKeys.one(turn.thread_id), (old) => {
    if (!old) return old
    const index = old.turns.findIndex((t) => t.id === turn.id)
    const turns = old.turns.slice()
    if (index === -1) {
      turns.push(turn)
    } else {
      turns[index] = turn
    }
    return { ...old, turns }
  })
}

export function usePostMessage(roomId: string) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (req: PostMessageRequest) => (await api.post<MessageResponse>(`/rooms/${roomId}/messages`, req)).message,
    onSuccess: (message) => applyRoomMessage(client, message),
  })
}

function updateRoom(client: QueryClient, roomId: string, fn: (pages: RoomMessage[][]) => RoomMessage[][]) {
  client.setQueryData<RoomPages>(messageKeys.room(roomId), (data) => {
    if (!data || data.pages.length === 0) {
      // Nothing loaded yet: the fetch that is coming will include it.
      return data
    }
    const pages = fn(data.pages)
    return pages === data.pages ? data : { ...data, pages }
  })
}

interface Location {
  page: number
  index: number
}

function locate(pages: RoomMessage[][], match: (m: RoomMessage) => boolean): Location | undefined {
  for (let page = 0; page < pages.length; page++) {
    const index = pages[page].findIndex(match)
    if (index !== -1) return { page, index }
  }
  return undefined
}

function replaceAt(pages: RoomMessage[][], at: Location, message: RoomMessage): RoomMessage[][] {
  const next = pages.slice()
  const row = pages[at.page].slice()
  row[at.index] = message
  next[at.page] = row
  return next
}

// The summary that arrives with a message event only names the thread
// (its counts are zero values), so a summary already known wins; a root
// seen for the first time takes the event's, which is right for a topic
// that has just opened.
function mergeSummary(existing: ThreadSummary | undefined, incoming: ThreadSummary | undefined): ThreadSummary | undefined {
  if (!incoming) return existing
  return existing ? { ...existing, id: incoming.id } : incoming
}
