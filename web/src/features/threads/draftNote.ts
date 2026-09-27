import type { t as translate } from '@/lib/i18n'
import type { Note } from './systemNote'

type T = typeof translate

// The notes of what members draft for a person to run (docs/design.md
// 5.23.5), as the hub writes them: the card, drawn from the draft itself
// where it is at hand (DraftCard), and what came of it, told to the member
// that drafted it.
const mergeCard = /^(.+?) drafted putting (.+?)'s work on the main line, as: ([\s\S]*)$/
const setAsideCard = /^(.+?) drafted giving (.+?)'s work up, since: ([\s\S]*)$/
const skillCard = /^(.+?) drafted installing the skill (\S+) for (.+)\.$/
// The leader's setup steps with a command, a card like any draft; its
// words are systemNote's (setup.pendingNote).
const stepsCard = /^.+ wrote down how a new worktree is got ready: .+\. A person adopts the command before it runs\.$/
const merged = /^(.+?) put (.+?)'s work on the main line as (.+?) drafted, as commit ([0-9a-f]+)\. Then, .+? said: ([\s\S]*)$/
const conflicted =
  /^(.+?) tried putting (.+?)'s work on the main line, as (.+?) drafted, but it conflicts with the main line in (.+?); nothing changed\. Then, .+? said: ([\s\S]*)$/
const setAside = /^(.+?) gave (.+?)'s work up as (.+?) drafted; it is kept as (\S+)\. Then, .+? said: ([\s\S]*)$/
const installed = /^(.+?) installed the skill (\S+) for (.+?) as (.+?) drafted\. Then, .+? said: ([\s\S]*)$/

// isDraftNote says a system note is the card of a draft.
export function isDraftNote(body: string): boolean {
  return mergeCard.test(body) || setAsideCard.test(body) || skillCard.test(body) || stepsCard.test(body)
}

// draftText is a note of a draft in the UI's words, or undefined for any
// other note.
export function draftText(t: T, body: string): Note | undefined {
  const m = mergeCard.exec(body)
  if (m) return { kind: 'draft', text: t('draft.mergeNote', { who: m[1], target: m[2] }), who: [m[1]] }
  const a = setAsideCard.exec(body)
  if (a) return { kind: 'draft', text: t('draft.setAsideNote', { who: a[1], target: a[2] }), who: [a[1]] }
  const s = skillCard.exec(body)
  if (s) return { kind: 'draft', text: t('draft.skillNote', { who: s[1], skill: s[2], target: s[3] }), who: [s[1]] }
  const done = merged.exec(body)
  if (done) {
    const [, person, target, drafter, commit, then] = done
    return { kind: 'merged', text: t('draft.mergedNote', { person, target, drafter, commit, then }), who: [person] }
  }
  const clash = conflicted.exec(body)
  if (clash) {
    const [, person, target, drafter, files, then] = clash
    return { kind: 'unresolved', text: t('draft.conflictedNote', { person, target, drafter, files: files.split(', ').join('、'), then }), who: [person] }
  }
  const aside = setAside.exec(body)
  if (aside) {
    const [, person, target, drafter, ref, then] = aside
    return { kind: 'setAside', text: t('draft.setAsideDoneNote', { person, target, drafter, ref, then }), who: [person] }
  }
  const skill = installed.exec(body)
  if (skill) {
    const [, person, name, target, drafter, then] = skill
    return { kind: 'installed', text: t('draft.installedNote', { person, skill: name, target, drafter, then }), who: [person] }
  }
  return undefined
}
