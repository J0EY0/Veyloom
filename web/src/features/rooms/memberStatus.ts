import type { StatusTone } from '@/components/shared/status-dot'
import { askKind, waitingKeys } from '@/features/approvals/kinds'
import type { MemberState, MemberStatus } from './useMemberStates'

// How a member's state reads and looks, shared by the island over the
// feed and the members panel beside it (docs/webui.md §4.4).
export const statusKey = {
  offline: 'member.offline',
  disabled: 'member.disabled',
  waiting: 'member.waiting',
  working: 'member.working',
  idle: 'member.idle',
} as const

// statusLabelKey is statusKey for one member's state, saying what a waiting
// member waits for: a permission, an answer, a form or a link.
export function statusLabelKey(state: Pick<MemberState, 'status' | 'approval'>) {
  return state.status === 'waiting' && state.approval ? waitingKeys[askKind(state.approval.kind)].member : statusKey[state.status]
}

export const statusTone: Record<MemberStatus, StatusTone> = {
  offline: 'idle',
  disabled: 'idle',
  waiting: 'wait',
  working: 'run',
  idle: 'idle',
}

// Who to look at first: whoever needs a person, then whoever is busy,
// then the rest, with the ones that cannot run at the bottom.
const rank: Record<MemberStatus, number> = { waiting: 0, working: 1, idle: 2, disabled: 3, offline: 4 }

export function byStatus(a: MemberStatus, b: MemberStatus): number {
  return rank[a] - rank[b]
}
