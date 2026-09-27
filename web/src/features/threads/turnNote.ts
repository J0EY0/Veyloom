import type { MessageKey } from '@/i18n/zh-CN'
import type { t as translate } from '@/lib/i18n'
import { turnErrorText } from '@/features/turns/turnError'
import type { Note } from './systemNote'

type T = typeof translate

// The notes of a member's session and turn, as the hub writes them: it
// starts over in a new session, and why (docs/design.md 5.6); a turn of it
// said nothing in its topic, even asked for its reply (5.24), was
// cancelled, or failed.
const newSession = /^(.+) started a new session \((.+)\) and no longer remembers its earlier turns; the chat history is still here for it\.$/
const silent = /^(.+) ended its turn without a word in this topic\.$/
// A turn a person cancelled, perhaps asking for a new session with it, and
// one over before the cancel reached it, the new session all the same
// (5.23.8); a turn that failed, with the hub's or the runtime's words.
const cancelled = /^(.+)'s turn was cancelled(; its next turn starts a new session)?$/
const startOver = /^(.+)'s next turn starts a new session, as a person asked\.$/
const failed = /^([^@\n]+?) failed: ([\s\S]+)$/

// Why a session ended, in the hub's words (sessionEndPhrase).
const sessionReasons: Record<string, MessageKey> = {
  'the agent now runs on a different runtime': 'session.reason.runtime',
  'the agent now runs on a different machine': 'session.reason.machine',
  'its working directory changed': 'session.reason.dir',
  'its role card changed': 'session.reason.roleCard',
  'the runtime no longer has it': 'session.reason.notFound',
  "it outgrew the model's context window": 'session.reason.overflow',
  'a person asked for a new one': 'session.reason.manual',
  'resuming it failed': 'session.reason.failed',
}

// turnText is a note of a member's session or turn in the UI's words, or
// undefined for any other note; a reason it does not know leaves the note
// as the hub wrote it.
export function turnText(t: T, body: string): Note | undefined {
  const started = newSession.exec(body)
  const reason = started ? sessionReasons[started[2]] : undefined
  if (started && reason) return { kind: 'session', text: t('session.note', { who: started[1], reason: t(reason) }), who: [started[1]] }
  const quiet = silent.exec(body)
  if (quiet) return { kind: 'silent', text: t('turn.silentNote', { who: quiet[1] }), who: [quiet[1]] }
  const stopped = cancelled.exec(body)
  if (stopped) {
    const text = t(stopped[2] ? 'turn.cancelledFreshNote' : 'turn.cancelledNote', { who: stopped[1] })
    return { kind: 'cancelled', text, who: [stopped[1]] }
  }
  const over = startOver.exec(body)
  if (over) return { kind: 'session', text: t('turn.startOverNote', { who: over[1] }), who: [over[1]] }
  const failure = failed.exec(body)
  if (failure) return { kind: 'failed', text: t('turn.failedNote', { who: failure[1], error: turnErrorText(failure[2]) }), who: [failure[1]] }
  return undefined
}
