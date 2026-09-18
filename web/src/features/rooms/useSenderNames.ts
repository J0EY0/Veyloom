import { useMemo } from 'react'
import { useRoomMembers } from '@/api/agents'
import type { Message, SenderKind } from '@/api/types'
import { useUsers } from '@/api/users'
import { useAgentLooks, type AgentLook } from '@/lib/agentLooks'
import { t } from '@/lib/i18n'

export interface Sender {
  name: string
  kind: SenderKind
  // The runtime behind an agent, shown as a small tag next to its name.
  runtime?: string
  // How the agent looks, for its avatar.
  look?: AgentLook
}

// Messages carry ids; names come from the users and the room's members.
// Returns a lookup built once per change of either list.
export function useSenderNames(roomId: string): (message: Message) => Sender {
  const users = useUsers()
  const members = useRoomMembers(roomId, { removed: true })
  const lookOf = useAgentLooks()

  return useMemo(() => {
    const userNames = new Map(users.data?.map((user) => [user.id, user.name]))
    const memberInfo = new Map(members.data?.map((member) => [member.id, { name: member.display_name, look: lookOf(member.agent_id) }]))
    return (message: Message): Sender => {
      switch (message.sender_kind) {
        case 'user':
          return { name: userNames.get(message.user_id ?? '') ?? t('sender.unknownUser'), kind: 'user' }
        case 'agent': {
          const info = memberInfo.get(message.member_id ?? '')
          return { name: info?.name ?? t('sender.unknownMember'), kind: 'agent', runtime: info?.look?.runtime, look: info?.look }
        }
        default:
          return { name: t('sender.system'), kind: 'system' }
      }
    }
  }, [users.data, members.data, lookOf])
}
