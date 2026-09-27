import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './client'
import { machineKeys } from './agents'
import type { Member, Pause, PausesResponse } from './types'

// What keeps turns from starting now (docs/design.md 5.23.3): an account's
// on a machine, or a member's. The inbox stream tells of each that comes
// or goes; polled besides, for one that runs out.
export const pauseKeys = {
  all: ['pauses'] as const,
}

export function usePauses() {
  return useQuery({
    queryKey: pauseKeys.all,
    queryFn: async () => (await api.get<PausesResponse>('/pauses')).pauses,
    staleTime: 30_000,
    refetchInterval: 60_000,
  })
}

// useLiftPause lifts a pause, and what it held up goes on.
export function useLiftPause() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete<void>(`/pauses/${id}`),
    onSettled: () => {
      void client.invalidateQueries({ queryKey: pauseKeys.all })
      void client.invalidateQueries({ queryKey: machineKeys.all })
    },
  })
}

// inEffect says a pause holds things up at now: one that ran out does not,
// whether or not the hub has lifted it yet.
export function inEffect(pause: Pause, now: number = Date.now()): boolean {
  return !pause.ends_at || Date.parse(pause.ends_at) > now
}

// pauseOf is the pause that holds a member up: its own, or else its
// account's, on its machine with its runtime.
export function pauseOf(
  pauses: Pause[] | undefined,
  member: Pick<Member, 'id' | 'machine_id'>,
  runtime: string | undefined,
  now: number = Date.now(),
): Pause | undefined {
  const live = (pauses ?? []).filter((pause) => inEffect(pause, now))
  return (
    live.find((pause) => pause.member_id === member.id) ??
    live.find((pause) => !pause.member_id && pause.machine_id === member.machine_id && runtime !== undefined && pause.runtime === runtime)
  )
}

// accountPause is the pause of a runtime's account on a machine.
export function accountPause(pauses: Pause[] | undefined, machineId: string, runtime: string, now: number = Date.now()): Pause | undefined {
  return (pauses ?? []).find((pause) => !pause.member_id && pause.machine_id === machineId && pause.runtime === runtime && inEffect(pause, now))
}
