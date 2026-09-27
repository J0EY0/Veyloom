import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { api } from './client'
import { errorText } from './errorText'
import { patchQuery } from './live'
import { applyTurn } from './messages'
import type { Turn, TurnResponse, TurnsResponse } from './types'

export const turnKeys = {
  one: (turnId: string) => ['turns', turnId] as const,
  running: (roomId: string) => ['rooms', roomId, 'turns', 'running'] as const,
}

export function useTurn(turnId: string) {
  return useQuery({
    queryKey: turnKeys.one(turnId),
    queryFn: async () => (await api.get<TurnResponse>(`/turns/${turnId}`)).turn,
    enabled: turnId !== '',
  })
}

// The turns in flight right now: what the status island is made of.
// Opening a room fetches them once; events keep the list current.
export function useRunningTurns(roomId: string) {
  return useQuery({
    queryKey: turnKeys.running(roomId),
    queryFn: async () => (await api.get<TurnsResponse>(`/rooms/${roomId}/turns?status=running`)).turns,
    enabled: roomId !== '',
  })
}

// applyRunningTurn keeps the running list and the turn's own entry in
// step with turn events. Whether a running turn is quiet is the hub's to
// say, with events of its own: a turn read from the store keeps what they
// said.
export function applyRunningTurn(client: QueryClient, turn: Turn) {
  patchQuery<Turn[]>(client, turnKeys.running(turn.room_id), (old) => {
    if (!old) return old
    const before = old.find((t) => t.id === turn.id)
    const rest = old.filter((t) => t.id !== turn.id)
    return turn.status === 'running' ? [{ ...turn, quiet_since: turn.quiet_since ?? before?.quiet_since }, ...rest] : rest
  })
  patchQuery<Turn>(client, turnKeys.one(turn.id), (old) =>
    old ? { ...turn, quiet_since: turn.status === 'running' ? (turn.quiet_since ?? old.quiet_since) : undefined } : old,
  )
}

// applyQuiet says a running turn went quiet, since since, or stirred again
// (docs/design.md 5.23.8).
export function applyQuiet(client: QueryClient, roomId: string, turnId: string, since: string | undefined) {
  patchQuery<Turn[]>(client, turnKeys.running(roomId), (old) => old?.map((t) => (t.id === turnId ? { ...t, quiet_since: since } : t)))
  patchQuery<Turn>(client, turnKeys.one(turnId), (old) => (old && old.status === 'running' ? { ...old, quiet_since: since } : old))
}

// Takes back letting the rest of a turn's requests through: people are
// asked again. The turn comes back as it now stands, and so does the
// turn_trust event.
export function useUntrustTurn() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (turnId: string) => (await api.delete<TurnResponse>(`/turns/${turnId}/trust`)).turn,
    onSuccess: (turn) => {
      applyTurn(client, turn)
      applyRunningTurn(client, turn)
    },
  })
}

// What cancelling a turn takes: which, and whether the member's next turn
// starts a new session (docs/design.md 5.23.8).
export interface CancelTurn {
  turnId: string
  newSession?: boolean
}

// Cancelling is asynchronous: the server answers 202 and the turn ends
// through the normal turn_finished event.
export function useCancelTurn() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: ({ turnId, newSession }: CancelTurn) => api.post<undefined>(`/turns/${turnId}/cancel`, newSession ? { new_session: true } : {}),
    onSuccess: (_data, { turnId }) => {
      void client.invalidateQueries({ queryKey: turnKeys.one(turnId) })
    },
    // Most often the turn ended meanwhile: say so, and show how it ended.
    onError: (err, { turnId }) => {
      toast.error(errorText(err))
      void client.invalidateQueries({ queryKey: turnKeys.one(turnId) })
    },
  })
}
