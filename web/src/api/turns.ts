import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { api } from './client'
import { errorText } from './errorText'
import { patchQuery } from './live'
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
// step with turn events.
export function applyRunningTurn(client: QueryClient, turn: Turn) {
  patchQuery<Turn[]>(client, turnKeys.running(turn.room_id), (old) => {
    if (!old) return old
    const rest = old.filter((t) => t.id !== turn.id)
    return turn.status === 'running' ? [turn, ...rest] : rest
  })
  patchQuery<Turn>(client, turnKeys.one(turn.id), (old) => (old ? turn : old))
}

// Cancelling is asynchronous: the server answers 202 and the turn ends
// through the normal turn_finished event.
export function useCancelTurn() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (turnId: string) => api.post<undefined>(`/turns/${turnId}/cancel`, {}),
    onSuccess: (_data, turnId) => {
      void client.invalidateQueries({ queryKey: turnKeys.one(turnId) })
    },
    // Most often the turn ended meanwhile: say so, and show how it ended.
    onError: (err, turnId) => {
      toast.error(errorText(err))
      void client.invalidateQueries({ queryKey: turnKeys.one(turnId) })
    },
  })
}
