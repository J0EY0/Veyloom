import type { Pause } from '@/api/types'
import type { StatusTone } from '@/components/shared/status-dot'
import { askKind, waitingKeys } from '@/features/approvals/kinds'
import { formatTime } from '@/lib/format'
import type { t as translate } from '@/lib/i18n'
import type { MemberState, MemberStatus } from './useMemberStates'

type T = typeof translate

// How a member's state reads and looks, shared by the island over the
// feed and the members panel beside it (docs/webui.md §4.4).
export const statusKey = {
  offline: 'member.offline',
  disabled: 'member.disabled',
  waiting: 'member.waiting',
  working: 'member.working',
  paused: 'member.paused',
  idle: 'member.idle',
} as const

// statusLabelKey is statusKey for one member's state, saying what a waiting
// member waits for: a permission, an answer, a form or a link.
export function statusLabelKey(state: Pick<MemberState, 'status' | 'approval'>) {
  return state.status === 'waiting' && state.approval ? waitingKeys[askKind(state.approval.kind)].member : statusKey[state.status]
}

// isQuiet says a working member's turn may be stuck: it showed no sign of
// life for a while, waiting on no person (docs/design.md 5.23.8).
export function isQuiet(state: Pick<MemberState, 'status' | 'turn'>): boolean {
  return state.status === 'working' && state.turn?.quiet_since !== undefined
}

// statusLabel is how one member's state reads: a paused member says why,
// and until when (docs/design.md 5.23.3), and one whose turn went quiet
// that it may be stuck.
export function statusLabel(t: T, state: Pick<MemberState, 'status' | 'approval' | 'pause' | 'turn'>): string {
  if (isQuiet(state)) return t('member.quiet')
  return state.status === 'paused' && state.pause ? pauseLabel(t, state.pause) : t(statusLabelKey(state))
}

// pauseLabel says what a pause holds up for, and until when: "额度用完 ·
// 16:23 恢复", "登录失效".
export function pauseLabel(t: T, pause: Pick<Pause, 'reason' | 'ends_at'>): string {
  const at = pause.ends_at ? formatTime(pause.ends_at) : ''
  switch (pause.reason) {
    case 'quota':
      return at ? t('pause.quotaUntil', { time: at }) : t('pause.quota')
    case 'auth':
      return t('pause.auth')
    case 'rate_limit':
      return t('pause.rateLimit', { time: at })
    case 'server':
      return t('pause.server', { time: at })
    default:
      return t('pause.failing')
  }
}

export const statusTone: Record<MemberStatus, StatusTone> = {
  offline: 'idle',
  disabled: 'idle',
  waiting: 'wait',
  working: 'run',
  paused: 'wait',
  idle: 'idle',
}

// toneOf is statusTone for one member's state: a turn gone quiet asks a
// person to look, as a wait does.
export function toneOf(state: Pick<MemberState, 'status' | 'turn'>): StatusTone {
  return isQuiet(state) ? 'wait' : statusTone[state.status]
}

// Who to look at first: whoever needs a person, then whoever is held up,
// then whoever is busy, then the rest, with the ones that cannot run at
// the bottom.
const rank: Record<MemberStatus, number> = { waiting: 0, paused: 1, working: 2, idle: 3, disabled: 4, offline: 5 }

export function byStatus(a: MemberStatus, b: MemberStatus): number {
  return rank[a] - rank[b]
}
