import { createContext, useContext, useMemo } from 'react'
import { useAgents } from '@/api/agents'

// How an agent looks: the picture uploaded for it, or else its runtime's
// mark. A member looks like its agent.
export interface AgentLook {
  avatar: string
  runtime: string
}

// useAgentLooks answers how an agent looks by its id, from the agents the
// page already reads: members and messages carry only an agent's id.
export function useAgentLooks(): (agentId: string | undefined) => AgentLook | undefined {
  const agents = useAgents()
  return useMemo(() => {
    const byId = new Map((agents.data ?? []).map((agent) => [agent.id, { avatar: agent.avatar, runtime: agent.runtime }]))
    return (agentId: string | undefined) => (agentId ? byId.get(agentId) : undefined)
  }, [agents.data])
}

// MemberLooks holds how a room's members look, by member id, for what
// knows only the member a mention names: the pills inside a message.
export const MemberLooks = createContext<ReadonlyMap<string, AgentLook>>(new Map())

export function useMemberLook(memberId: string | undefined): AgentLook | undefined {
  const looks = useContext(MemberLooks)
  return memberId ? looks.get(memberId) : undefined
}
