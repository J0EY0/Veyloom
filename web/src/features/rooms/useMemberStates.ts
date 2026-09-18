import { useMemo } from 'react'
import { useMachines, useRoomMembers } from '@/api/agents'
import { usePendingApprovals } from '@/api/approvals'
import { useRunningTurns } from '@/api/turns'
import type { Approval, Member, Turn } from '@/api/types'
import { useAgentLooks, type AgentLook } from '@/lib/agentLooks'

export type MemberStatus = 'offline' | 'disabled' | 'waiting' | 'working' | 'idle'

export interface MemberState {
  member: Member
  // How its agent looks, for its avatar; unknown until the agents load.
  look?: AgentLook
  status: MemberStatus
  // The turn in flight, when working or waiting.
  turn?: Turn
  // The request waiting for a person, when waiting.
  approval?: Approval
}

// useMemberStates derives each member's state from four lists the UI has
// anyway (docs/webui.md §4.4): its machine being connected, its enabled
// flag, a request of its waiting for a person, and a turn of its running.
export function useMemberStates(roomId: string): MemberState[] {
  const members = useRoomMembers(roomId)
  const running = useRunningTurns(roomId)
  const pending = usePendingApprovals(roomId)
  const machines = useMachines()
  const lookOf = useAgentLooks()

  return useMemo(() => {
    const online = machines.data ? new Set(machines.data.map((machine) => machine.id)) : undefined
    const turnOf = new Map((running.data ?? []).map((turn) => [turn.member_id, turn]))
    const approvalOf = new Map<string, Approval>()
    for (const approval of pending.data ?? []) {
      if (!approvalOf.has(approval.member_id)) approvalOf.set(approval.member_id, approval)
    }
    return (members.data ?? []).map((member) => {
      const turn = turnOf.get(member.id)
      const approval = approvalOf.get(member.id)
      let status: MemberStatus = 'idle'
      if (online && !online.has(member.machine_id)) status = 'offline'
      else if (!member.enabled) status = 'disabled'
      else if (approval) status = 'waiting'
      else if (turn) status = 'working'
      return { member, look: lookOf(member.agent_id), status, turn, approval }
    })
  }, [members.data, running.data, pending.data, machines.data, lookOf])
}
