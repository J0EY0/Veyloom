import { useMemo } from 'react'
import { useRoomMembers } from '@/api/agents'
import type { Mention } from '@/api/types'
import { useUsers } from '@/api/users'
import { useAgentLooks, type AgentLook } from '@/lib/agentLooks'

export interface MentionTarget {
  mention: Mention
  name: string
  // How a member looks, for its avatar; people go by their initial.
  look?: AgentLook
}

export interface MentionTargets {
  // Who can be @-mentioned from the composer: the room's enabled members.
  members: MentionTarget[]
  // Display names for everyone a message may mention, by id, members taken
  // out of the project included.
  names: Map<string, string>
  // How each member looks, by member id, those taken out included.
  looks: ReadonlyMap<string, AgentLook>
  // Every target by name, for turning "@Name" in a body into a mention.
  all: MentionTarget[]
}

const empty: MentionTargets = { members: [], names: new Map(), looks: new Map(), all: [] }

// useMentionTargets joins the room's members and the users into the
// lookups mention rendering and the @ picker need.
export function useMentionTargets(roomId: string): MentionTargets {
  const members = useRoomMembers(roomId, { removed: true })
  const users = useUsers()
  const lookOf = useAgentLooks()
  return useMemo(() => {
    if (!members.data && !users.data) return empty
    const looks = new Map<string, AgentLook>()
    for (const member of members.data ?? []) {
      const look = lookOf(member.agent_id)
      if (look) looks.set(member.id, look)
    }
    const memberTargets: MentionTarget[] = (members.data ?? [])
      .filter((member) => member.enabled && !member.removed_at)
      .map((member) => ({ mention: { kind: 'agent', id: member.id }, name: member.display_name, look: looks.get(member.id) }))
    const userTargets: MentionTarget[] = (users.data ?? []).map((user) => ({
      mention: { kind: 'user', id: user.id },
      name: user.name,
    }))
    const names = new Map<string, string>()
    for (const member of members.data ?? []) names.set(member.id, member.display_name)
    for (const user of users.data ?? []) names.set(user.id, user.name)
    return { members: memberTargets, names, looks, all: [...memberTargets, ...userTargets] }
  }, [members.data, users.data, lookOf])
}

// detectMentions finds every target whose "@Name" appears in body, so
// what is sent as structure always matches what was typed. At each @ the
// longest name that follows is the one meant, as the hub reads agents: with
// Coder and Coder2 in the room "@Coder2" names Coder2 alone. names are the
// others an @ may mean, such as a member taken out of the project.
export function detectMentions(body: string, targets: MentionTarget[], names: Iterable<string> = []): Mention[] {
  const meant = namedAt(body, [...targets.map((target) => target.name), ...names])
  const seen = new Set<string>()
  const out: Mention[] = []
  for (const target of targets) {
    const key = `${target.mention.kind}:${target.mention.id}`
    if (!seen.has(key) && meant.has(target.name)) {
      seen.add(key)
      out.push(target.mention)
    }
  }
  return out
}

// namedAt is the names text @-mentions, the longest one after each @.
export function namedAt(text: string, names: string[]): Set<string> {
  const longest = [...new Set(names)].filter((name) => name !== '').sort((a, b) => b.length - a.length)
  const meant = new Set<string>()
  for (let at = text.indexOf('@'); at >= 0; at = text.indexOf('@', at + 1)) {
    const name = longest.find((n) => text.startsWith(n, at + 1))
    if (name !== undefined) meant.add(name)
  }
  return meant
}
