import type { MessageKey } from '@/i18n/zh-CN'
import { formatLongSpan, formatTime } from '@/lib/format'
import type { t as translate } from '@/lib/i18n'
import { runtimeName } from '@/lib/runtimes'
import { draftText } from './draftNote'
import { turnText } from './turnNote'

type T = typeof translate

// The system notes the hub writes in English that the UI says its own way;
// the rest are shown as the hub wrote them.
// An upkeep's note (docs/design.md 5.12, 5.16): who, why, and what it goes
// over; what people said came in later, so older notes lack it.
const upkeepNote =
  /^Wiki upkeep by (.+) \((topics went quiet|daily|every three days|weekly|the last upkeep left turns to go over|asked by a person)\): (\d+) turns? of this chat, (\d+) turns? of other projects using this team's skills(?:, (\d+) messages? from people)?\.$/

const upkeepReasons: Record<string, MessageKey> = {
  'topics went quiet': 'upkeep.reason.quiet',
  daily: 'upkeep.reason.daily',
  'every three days': 'upkeep.reason.every3days',
  weekly: 'upkeep.reason.weekly',
  'the last upkeep left turns to go over': 'upkeep.reason.backlog',
  'asked by a person': 'upkeep.reason.asked',
}

// The notes of a project's setup and of its members' branches
// (docs/design.md 5.21).
const setupNote = /^Project setup by (.+) \((a member needs a worktree|asked by a person)\)\.$/
const pendingNote = /^(.+) wrote down how a new worktree is got ready: (.+)\. A person adopts the command before it runs\.$/
// A member's work merged: whose, whose it had in it, and the message's
// first line (the hub's first notes had the whole message).
const mergedNote = /^Merged (.+?)'s work(?:, which had (.+) in it,)? into the main line as ([0-9a-f]+): ([\s\S]+)$/
const prepareFailed = /^@(.+?) getting (.+)'s worktree ready failed: (.+)\.$/
// Members who changed the same files, the one whose turn found it first;
// the files named, and how many more there are.
const overlapNote = /^(.+) (?:both|all) changed (.+?)(?: and (\d+) more)?: whichever is merged second may conflict\.$/

// A wake of one member by another's message that a limit held back
// (docs/design.md 5.22), addressed to the person: the piece of work reached
// its relay limit, or the last turns agents woke only talked.
const holdNote =
  /^(?:@\S+ )?(.+) mentioned (.+), but (?:agents have woken (\d+) turns in this piece of work since a person last spoke|the last (\d+) turns agents woke in this piece of work only talked); it waits for a person now\.$/

// A member's reminder to itself (docs/design.md 5.23.4): set, due at an
// RFC 3339 time; come due, late by the hub's span when the hub or the
// machine was away; and its wake held back, as a wake by another's
// message is, or since only people wake members in the project.
const reminderSetNote = /^(.+?) set a reminder for (\S+?): ([\s\S]*)$/
const reminderDueNote = /^(.+?)'s reminder, due (\S+?)(?: \((\S+) late\))?: ([\s\S]*)$/
const reminderHoldNote =
  /^(?:@\S+ )?(.+)'s reminder came due, but (?:agents have woken (\d+) turns in this piece of work since a person last spoke|the last (\d+) turns agents woke in this piece of work only talked|only people wake members in this project); it waits for a person now\.$/

// isHoldNote says a system note tells of a wake a limit held back.
export function isHoldNote(body: string): boolean {
  return holdNote.test(body) || reminderHoldNote.test(body)
}

// isReminderNote says a system note tells of a member setting a reminder.
export function isReminderNote(body: string): boolean {
  return reminderSetNote.test(body)
}

// spanMs reads the hub's span, "2d4h" or "45m", as milliseconds; one it
// does not know is none.
export function spanMs(span: string): number {
  const parts = /^(?:(\d+)d)?(?:(\d+)h)?(?:(\d+)m)?$/.exec(span)
  if (!parts || span === '') return 0
  return ((Number(parts[1] ?? 0) * 24 + Number(parts[2] ?? 0)) * 60 + Number(parts[3] ?? 0)) * 60_000
}

// reminderText is a note of a reminder in the UI's words, or undefined
// for any other note.
function reminderText(t: T, body: string): Note | undefined {
  const set = reminderSetNote.exec(body)
  if (set) return { kind: 'reminder', text: t('reminder.setNote', { who: set[1], time: formatTime(set[2]), note: set[3] }), who: [set[1]] }
  const due = reminderDueNote.exec(body)
  if (due) {
    const [, who, , late, note] = due
    const text = late ? t('reminder.dueLateNote', { who, note, late: formatLongSpan(spanMs(late)) }) : t('reminder.dueNote', { who, note })
    return { kind: 'reminderDue', text, who: [who] }
  }
  const held = reminderHoldNote.exec(body)
  if (held) {
    const [, who, woken, idle] = held
    const text =
      woken !== undefined
        ? t('relay.reminderLimitNote', { who, n: woken })
        : idle !== undefined
          ? t('relay.reminderIdleNote', { who, n: idle })
          : t('relay.reminderPeopleNote', { who })
    return { kind: 'hold', text, who: [who] }
  }
  return undefined
}

// The work a member handed on is done, and goes back to it (docs/design.md
// 5.22); the hub's first wording said it sums it up.
const sumUpNote = /^The work (.+) handed on is done \((.+)\); (?:back to \1|\1 sums it up)\.$/

// What keeps a member from answering (docs/design.md 5.23.3): its
// account's usage limit reached, signed out, too many requests, its
// provider failing, or its own turns failing; the times as RFC 3339.
const quotaUntilNote = /^(.+) waits for (\S+)'s usage limit on (.+) to reset at (\S+)\.$/
const quotaNote = /^(.+) waits: (\S+)'s usage limit on (.+) is reached; resume it once the account has more\.$/
const authNote = /^(.+) waits: (\S+) is signed out on (.+); sign it in again there, then resume it\.$/
const rateLimitNote = /^(.+) waits: (\S+) on (.+) was turned down for too many requests; it tries again at (\S+)\.$/
const serverNote = /^(.+) waits: (\S+)'s service failed on (.+); it tries again at (\S+)\.$/
const failingNote = /^(.+) waits: its last (\d+) turns failed; resume it once what failed is seen to\.$/

// pauseText is a note of a pause in the UI's words, or undefined for any
// other note.
function pauseText(t: T, body: string): Note | undefined {
  const quotaUntil = quotaUntilNote.exec(body)
  if (quotaUntil) {
    const [, who, runtime, machine, at] = quotaUntil
    return { kind: 'paused', text: t('pause.note.quotaUntil', { who, runtime: runtimeName(runtime), machine, time: formatTime(at) }), who: [who] }
  }
  const quota = quotaNote.exec(body)
  if (quota) return { kind: 'paused', text: t('pause.note.quota', { who: quota[1], runtime: runtimeName(quota[2]), machine: quota[3] }), who: [quota[1]] }
  const auth = authNote.exec(body)
  if (auth) return { kind: 'paused', text: t('pause.note.auth', { who: auth[1], runtime: runtimeName(auth[2]), machine: auth[3] }), who: [auth[1]] }
  const limited = rateLimitNote.exec(body)
  if (limited) {
    const [, who, runtime, machine, at] = limited
    return { kind: 'paused', text: t('pause.note.rateLimit', { who, runtime: runtimeName(runtime), machine, time: formatTime(at) }), who: [who] }
  }
  const server = serverNote.exec(body)
  if (server) {
    const [, who, runtime, machine, at] = server
    return { kind: 'paused', text: t('pause.note.server', { who, runtime: runtimeName(runtime), machine, time: formatTime(at) }), who: [who] }
  }
  const failing = failingNote.exec(body)
  if (failing) return { kind: 'paused', text: t('pause.note.failing', { who: failing[1], n: failing[2] }), who: [failing[1]] }
  return undefined
}

// A merge a member left under way, seen to as its turn ended: committed
// for it, or still with conflicts in the files named.
const concludedNote = /^(.+) settled the conflicts but left the merge uncommitted; Veyloom committed it as ([0-9a-f]+)\.$/
const unresolvedNote = /^(.+)'s worktree is still in the middle of a merge: (.+) still (?:has|have) conflict markers\.$/

// namesIn reads the hub's "A and B" or "A, B and C".
function namesIn(list: string): string[] {
  const last = list.lastIndexOf(' and ')
  return last < 0 ? [list] : [...list.slice(0, last).split(', '), list.slice(last + ' and '.length)]
}

// stepsText says the hub's "copy a, b, then run c" in the UI's words.
export function stepsText(t: T, steps: string): string {
  const run = /^run (.+)$/.exec(steps)
  if (run) return t('setup.steps.run', { command: run[1] })
  const copy = /^copy (.+?)(?:, then run (.+))?$/.exec(steps)
  if (!copy) return steps
  const copied = t('setup.steps.copy', { paths: copy[1] })
  return copy[2] ? copied + t('setup.steps.then') + t('setup.steps.run', { command: copy[2] }) : copied
}

// The hub's own notices in a turn whose worktree is being got ready.
const makingNotice = /^Making (.+)'s worktree, on the branch (.+)\.$/
const preparingNotice = /^Getting the worktree ready: (.+)\.$/

// noticeText is a notice the hub showed in a turn, in the UI's words; a
// runtime's are shown as it said them. The hub's say what it is doing
// before the turn begins: once past, done says so, and they read as done.
export function noticeText(t: T, text: string, done = false): string {
  if (text === "Waiting for the project's leader to set the project up for worktrees.") return t(done ? 'setup.notice.waitedLeader' : 'setup.notice.waitLeader')
  if (text === 'Waiting for a person to adopt the setup steps the leader wrote down.') return t(done ? 'setup.notice.waitedPerson' : 'setup.notice.waitPerson')
  const making = makingNotice.exec(text)
  if (making) return t(done ? 'setup.notice.made' : 'setup.notice.making', { name: making[1], branch: making[2] })
  const preparing = preparingNotice.exec(text)
  if (preparing) return t(done ? 'setup.notice.prepared' : 'setup.notice.preparing', { steps: stepsText(t, preparing[1]) })
  return text
}

// A person committed what was changed in the project's checkout.
const committedNote = /^Committed the changes in the project's checkout as ([0-9a-f]+): ([\s\S]+)$/
// A person set a member's work aside, kept in git under the ref named.
const resetNote = /^Reset (.+)'s branch (\S+) to the main line; its work is archived as (\S+)\.$/
// The wording before 2026-09-26, still in older chats.
const setAsideNote = /^Set aside (.+)'s work, kept in git as (\S+): its worktree starts over from the main line\.$/

// What a system note is about: it picks the note's mark and colour.
export type NoteKind =
  | 'setup'
  | 'steps'
  | 'merged'
  | 'committed'
  | 'setAside'
  | 'overlap'
  | 'hold'
  | 'sumUp'
  | 'concluded'
  | 'unresolved'
  | 'failed'
  | 'upkeep'
  | 'paused'
  | 'reminder'
  | 'reminderDue'
  | 'draft'
  | 'installed'
  | 'silent'
  | 'session'
  | 'cancelled'
  | 'other'

// A system note in the UI's words: what it is about, and who it is about,
// the names the line brings forward.
export interface Note {
  kind: NoteKind
  text: string
  who: string[]
}

// systemNote reads a system note the hub wrote.
export function systemNote(t: T, body: string): Note {
  const setup = setupNote.exec(body)
  if (setup) {
    const reason = t(setup[2] === 'asked by a person' ? 'setup.reason.asked' : 'setup.reason.worktree')
    return { kind: 'setup', text: t('setup.note', { who: setup[1], reason }), who: [setup[1]] }
  }
  const pending = pendingNote.exec(body)
  if (pending) return { kind: 'steps', text: t('setup.pendingNote', { who: pending[1], steps: stepsText(t, pending[2]) }), who: [pending[1]] }
  const merged = mergedNote.exec(body)
  if (merged) {
    const [, who, had, commit, message] = merged
    const held = had ? namesIn(had).map((name) => name.replace(/'s$/, '')) : []
    const params = { who, commit, message: message.split('\n')[0], held: held.join(t('common.listSeparator')) }
    return { kind: 'merged', text: t(held.length > 0 ? 'branches.mergedWithNote' : 'branches.mergedNote', params), who: [who, ...held] }
  }
  const committed = committedNote.exec(body)
  if (committed) return { kind: 'committed', text: t('branches.committedNote', { commit: committed[1], message: committed[2] }), who: [] }
  const reset = resetNote.exec(body)
  if (reset) return { kind: 'setAside', text: t('branches.setAsideNote', { who: reset[1], branch: reset[2], ref: reset[3] }), who: [reset[1]] }
  const aside = setAsideNote.exec(body)
  if (aside) return { kind: 'setAside', text: t('branches.setAsideNoteOld', { who: aside[1], ref: aside[2] }), who: [aside[1]] }
  const overlap = overlapNote.exec(body)
  if (overlap) {
    const named = overlap[2].split(', ')
    const more = Number(overlap[3] ?? 0)
    const files = more > 0 ? t('branches.overlapMore', { files: named.join(t('common.listSeparator')), n: named.length + more }) : named.join(t('common.listSeparator'))
    const names = namesIn(overlap[1]).join(t('common.listSeparator'))
    return { kind: 'overlap', text: t('branches.overlapNote', { names, files }), who: [names] }
  }
  const hold = holdNote.exec(body)
  if (hold) {
    const text =
      hold[3] !== undefined
        ? t('relay.limitNote', { waker: hold[1], woken: hold[2], n: hold[3] })
        : t('relay.idleNote', { waker: hold[1], woken: hold[2], n: hold[4] })
    return { kind: 'hold', text, who: [hold[1]] }
  }
  const sumUp = sumUpNote.exec(body)
  if (sumUp) {
    const names = sumUp[2].split(', ').join(t('common.listSeparator'))
    return { kind: 'sumUp', text: t('relay.sumUpNote', { who: sumUp[1], names }), who: [names] }
  }
  const concluded = concludedNote.exec(body)
  if (concluded) return { kind: 'concluded', text: t('branches.concludedNote', { who: concluded[1], commit: concluded[2] }), who: [concluded[1]] }
  const unresolved = unresolvedNote.exec(body)
  if (unresolved) {
    return { kind: 'unresolved', text: t('branches.unresolvedNote', { who: unresolved[1], files: unresolved[2].split(', ').join(t('common.listSeparator')) }), who: [unresolved[1]] }
  }
  const failed = prepareFailed.exec(body.split('\n')[0])
  if (failed) return { kind: 'failed', text: t('setup.prepareFailed', { leader: failed[1], member: failed[2], error: failed[3] }), who: [] }
  const paused = pauseText(t, body)
  if (paused) return paused
  const reminder = reminderText(t, body)
  if (reminder) return reminder
  const draft = draftText(t, body)
  if (draft) return draft
  const turn = turnText(t, body)
  if (turn) return turn
  const upkeep = upkeepNote.exec(body)
  if (upkeep) {
    const note = t('upkeep.note', { who: upkeep[1], reason: t(upkeepReasons[upkeep[2]]), own: upkeep[3], uses: upkeep[4] })
    return { kind: 'upkeep', text: upkeep[5] !== undefined ? note + t('upkeep.notePeople', { people: upkeep[5] }) : note, who: [upkeep[1]] }
  }
  return { kind: 'other', text: body, who: [] }
}

// systemText is a system note in the UI's words.
export function systemText(t: T, body: string): string {
  return systemNote(t, body).text
}
