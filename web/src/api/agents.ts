import { keepPreviousData, useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { browserTimeZone } from '@/lib/format'
import { addresseeKeys, refreshAddressees } from './addressee'
import { ApiError, api } from './client'
import { projectKeys } from './projects'
import { upkeepKeys } from './upkeep'
import type {
  Agent,
  AgentRequest,
  AgentResponse,
  AgentsResponse,
  Member,
  MemberResponse,
  MemberSessionResponse,
  MembersResponse,
  CreateMemberRequest,
  UpdateMemberRequest,
  Machine,
  ActivityRange,
  MachineActivityResponse,
  MachineMembersResponse,
  MachinesResponse,
} from './types'

export const memberKeys = {
  room: (roomId: string) => ['rooms', roomId, 'members'] as const,
  session: (memberId: string) => ['members', memberId, 'session'] as const,
}

export const agentKeys = {
  all: ['agents'] as const,
}

export const machineKeys = {
  all: ['machines'] as const,
}

// A room's current members. With removed, also the members taken out of the
// project: their messages, turns and mentions still need a name. Both read
// the one cached list.
export function useRoomMembers(roomId: string, { removed = false }: { removed?: boolean } = {}) {
  return useQuery({
    queryKey: memberKeys.room(roomId),
    queryFn: async () => (await api.get<MembersResponse>(`/rooms/${roomId}/members`)).members,
    enabled: roomId !== '',
    select: removed ? undefined : currentMembers,
  })
}

function currentMembers(members: Member[]): Member[] {
  return members.filter((member) => !member.removed_at)
}

export function useAgents() {
  return useQuery({
    queryKey: agentKeys.all,
    queryFn: async () => (await api.get<AgentsResponse>('/agents')).agents,
    staleTime: 5 * 60_000,
  })
}

async function fetchMachines(): Promise<Machine[]> {
  return (await api.get<MachinesResponse>('/machines')).machines
}

// Machines come and go; the list is what says whether an agent is reachable.
export function useMachines() {
  return useQuery({
    queryKey: machineKeys.all,
    queryFn: fetchMachines,
    staleTime: 30_000,
    refetchInterval: 60_000,
  })
}

// The current members a machine runs, across every project. Polled: the
// machines page has no event stream of its own.
export function useMachineMembers(machineId: string) {
  return useQuery({
    queryKey: [...machineKeys.all, machineId, 'members'],
    queryFn: async () => (await api.get<MachineMembersResponse>(`/machines/${machineId}/members`)).members,
    enabled: machineId !== '',
    refetchInterval: 10_000,
  })
}

// A machine's turns over the last day, hour by hour.
export interface ActivityOptions {
  // The last 24 hours when left out.
  range?: ActivityRange
  // One runtime's turns only; all of them when left out.
  runtime?: string
}

// What a machine did over a range, in hours and days that begin where the
// browser is. Switching range or runtime keeps the chart on screen until
// the new one arrives.
export function useMachineActivity(machineId: string, { range = '24h', runtime = '' }: ActivityOptions = {}) {
  return useQuery({
    queryKey: [...machineKeys.all, machineId, 'activity', range, runtime],
    queryFn: async () => {
      const params = new URLSearchParams({ range, tz: browserTimeZone() })
      if (runtime) params.set('runtime', runtime)
      return (await api.get<MachineActivityResponse>(`/machines/${machineId}/activity?${params}`)).activity
    },
    enabled: machineId !== '',
    refetchInterval: 60_000,
    placeholderData: keepPreviousData,
  })
}

// ProbeTimeout names the machines that had not answered when time ran out.
export class ProbeTimeout extends Error {
  readonly names: string[]
  constructor(names: string[]) {
    super(`no answer from ${names.join(', ')}`)
    this.names = names
  }
}

// Asks every given machine to look for its runtimes again, after something
// was installed or signed in, and waits until each has answered: a machine
// has once its probed_at moves. The fresh list lands in the cache on every
// look, so the page fills in as answers arrive. A machine that went away
// meanwhile is not waited for.
export function useProbeMachines({ intervalMs = 1000, timeoutMs = 30_000 } = {}) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (machines: Machine[]) => {
      const asked = new Map(machines.map((machine) => [machine.id, machine.probed_at]))
      await Promise.all(
        machines.map(async (machine) => {
          try {
            await api.post<undefined>(`/machines/${machine.id}/probe`, {})
          } catch (err) {
            if (!(err instanceof ApiError && err.status === 404)) throw err
          }
        }),
      )
      const deadline = Date.now() + timeoutMs
      for (;;) {
        const fresh = await client.fetchQuery({ queryKey: machineKeys.all, queryFn: fetchMachines, staleTime: 0 })
        const waiting = fresh.filter((machine) => asked.get(machine.id) === machine.probed_at)
        if (waiting.length === 0) return fresh
        if (Date.now() >= deadline) throw new ProbeTimeout(waiting.map((machine) => machine.name))
        await new Promise((resolve) => setTimeout(resolve, intervalMs))
      }
    },
  })
}

// Who leads a project, and so keeps its wiki unless someone else was
// chosen, follows its members (docs/design.md 5.21): a member joining,
// switched on or off, or taken out may change it. So may whom a message
// without an @ goes to, in the room and its topics (4.2).
function refreshLeaders(client: QueryClient, roomId: string) {
  void client.invalidateQueries({ queryKey: projectKeys.all })
  void client.invalidateQueries({ queryKey: upkeepKeys.all })
  refreshAddressees(client, addresseeKeys.room(roomId))
}

export function useCreateMember(roomId: string) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (req: CreateMemberRequest) => (await api.post<MemberResponse>(`/rooms/${roomId}/members`, req)).member,
    onSuccess: (member) => {
      client.setQueryData<Member[]>(memberKeys.room(roomId), (old) => (old ? [...old, member] : old))
      refreshLeaders(client, roomId)
    },
  })
}

export function useUpdateMember(roomId: string) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, patch }: { id: string; patch: UpdateMemberRequest }) => (await api.patch<MemberResponse>(`/members/${id}`, patch)).member,
    onSuccess: (member) => {
      client.setQueryData<Member[]>(memberKeys.room(roomId), (old) => (old ? old.map((m) => (m.id === member.id ? member : m)) : old))
      refreshLeaders(client, roomId)
    },
  })
}

// The session the member's next turn continues. Read when someone opens
// the member, never polled: it changes with every turn and nobody watches it.
export function useMemberSession(memberId: string) {
  return useQuery({
    queryKey: memberKeys.session(memberId),
    queryFn: () => api.get<MemberSessionResponse>(`/members/${memberId}/session`),
    enabled: memberId !== '',
    staleTime: 0,
  })
}

// Ends the member's session so that its next turn starts a new one. A 409
// means a turn of its is running, left for the caller to explain.
export function useResetMemberSession() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (memberId: string) => api.delete(`/members/${memberId}/session`),
    onSuccess: (_, memberId) => {
      client.setQueryData<MemberSessionResponse>(memberKeys.session(memberId), { turns: 0 })
    },
  })
}

// Takes a member out of its project. It stays in the cached list, marked,
// so what it said keeps a name. One already gone counts as done; a 409
// means a turn of its is running, left for the dialog to explain.
export function useRemoveMember(roomId: string) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (id: string) => {
      try {
        await api.delete(`/members/${id}`)
      } catch (err) {
        if (!(err instanceof ApiError && err.status === 404)) throw err
      }
      return id
    },
    onSuccess: (id) => {
      const now = new Date().toISOString()
      client.setQueryData<Member[]>(memberKeys.room(roomId), (old) =>
        old ? old.map((m) => (m.id === id ? { ...m, removed_at: m.removed_at ?? now } : m)) : old,
      )
      refreshLeaders(client, roomId)
    },
  })
}

export function useCreateAgent() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (req: AgentRequest) => (await api.post<AgentResponse>('/agents', req)).agent,
    onSuccess: (agent) => {
      client.setQueryData<Agent[]>(agentKeys.all, (old) => (old ? [...old, agent] : old))
    },
  })
}

export function useUpdateAgent() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, ...req }: AgentRequest & { id: string }) => (await api.put<AgentResponse>(`/agents/${id}`, req)).agent,
    onSuccess: (agent) => {
      client.setQueryData<Agent[]>(agentKeys.all, (old) => (old ? old.map((a) => (a.id === agent.id ? agent : a)) : old))
    },
  })
}

// Deleting one that is already gone counts as done. A 409 means it is still
// a member of some project; the error is left for the dialog to explain.
export function useDeleteAgent() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (id: string) => {
      try {
        await api.delete(`/agents/${id}`)
      } catch (err) {
        if (!(err instanceof ApiError && err.status === 404)) throw err
      }
      return id
    },
    onSuccess: (id) => {
      client.setQueryData<Agent[]>(agentKeys.all, (old) => (old ? old.filter((a) => a.id !== id) : old))
    },
  })
}
