import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { api } from './client'
import { refresh } from './refresh'
import { noWikiReadOnly, type RuntimeTraits } from './runtimes'
import type { Agent, Member, UpkeepResponse, UpkeepStatus } from './types'

// A project's wiki maintainer (docs/design.md 5.12): how its upkeep
// stands, and a person asking for one now.

export const upkeepKeys = {
  all: ['upkeep'] as const,
  project: (projectId: string) => ['upkeep', projectId] as const,
}

// upkeepBusy says an upkeep runs or waits for its member.
export function upkeepBusy(status?: UpkeepStatus): boolean {
  return !!status && (status.queued || status.last?.status === 'running')
}

// useUpkeepStatus is how a project's upkeep stands; looked at more often
// while one is on its way.
export function useUpkeepStatus(projectId: string) {
  return useQuery({
    queryKey: upkeepKeys.project(projectId),
    queryFn: async () => (await api.get<UpkeepResponse>(`/projects/${projectId}/wiki/maintainer`)).upkeep,
    enabled: projectId !== '',
    refetchInterval: (query) => (upkeepBusy(query.state.data) ? 5_000 : 60_000),
  })
}

// useStartUpkeep runs the project's maintainer now.
export function useStartUpkeep(projectId: string) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async () => (await api.post<UpkeepResponse>(`/projects/${projectId}/wiki/maintain`, {})).upkeep,
    onSuccess: (status) => client.setQueryData(upkeepKeys.project(projectId), status),
  })
}

// invalidateUpkeep has every upkeep status read again: a turn started or
// ended, an upkeep or one for an upkeep to go over.
export function invalidateUpkeep(client: QueryClient) {
  refresh(client, { queryKey: upkeepKeys.all })
}

// keepsNoWiki says a member would keep no wiki, its runtime writing
// nothing read-only it is not told to in so many words (docs/design.md
// 5.12): the runtime's name then, for saying so.
export function keepsNoWiki(member: Member | undefined, agents: Agent[] | undefined, traits: Record<string, RuntimeTraits> | undefined): string | undefined {
  const agent = agents?.find((a) => a.id === member?.agent_id)
  if (!member || !agent) return undefined
  return noWikiReadOnly(traits, agent.runtime, member.permission_preset || agent.permission_preset) ? agent.runtime : undefined
}
