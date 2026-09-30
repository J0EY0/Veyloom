import type { WikiPageInfo, WikiTeam } from '@/api/types'
import type { MessageKey } from '@/i18n/zh-CN'
import type { t as translate } from '@/lib/i18n'
import { runtimeName } from '@/lib/runtimes'
import type { RelationKey } from './graph/model'

type T = typeof translate

// What the wiki's words are called on screen (docs/design.md 5.3). A type
// the wiki does not know, from a page written by hand, shows as written.

const types: Record<string, { one: MessageKey; many: MessageKey }> = {
  Decision: { one: 'wiki.type.decision', many: 'wiki.types.decision' },
  Convention: { one: 'wiki.type.convention', many: 'wiki.types.convention' },
  Fact: { one: 'wiki.type.fact', many: 'wiki.types.fact' },
  Pitfall: { one: 'wiki.type.pitfall', many: 'wiki.types.pitfall' },
  Module: { one: 'wiki.type.module', many: 'wiki.types.module' },
  Topic: { one: 'wiki.type.topic', many: 'wiki.types.topic' },
  Pattern: { one: 'wiki.type.pattern', many: 'wiki.types.pattern' },
  Skill: { one: 'wiki.type.skill', many: 'wiki.types.skill' },
}

// typeName is one page's type: 决定, a Decision.
export function typeName(t: T, type: string): string {
  return types[type] ? t(types[type].one) : type || t('wiki.type.unknown')
}

// typeGroup heads the list of pages of a type: 决定, Decisions.
export function typeGroup(t: T, type: string): string {
  return types[type] ? t(types[type].many) : type || t('wiki.type.unknown')
}

// actorName is who an OKF actor is, as people call them: human:alice is
// alice, codex/default is Codex, claude-code/haiku is Claude Code · haiku.
export function actorName(actor: string): string {
  if (actor.startsWith('human:')) return actor.slice('human:'.length)
  if (actor.startsWith('process:')) {
    const name = actor.slice('process:'.length)
    return name === 'veyloom' ? 'Veyloom' : name
  }
  const [producer, ...rest] = actor.split('/')
  const model = rest.join('/')
  const name = runtimeName(producer === 'claude-code' ? 'claude' : producer)
  return model && model !== 'default' ? `${name} · ${model}` : name
}

// producerName is who wrote or checked a page as its byline says it: a
// person or a process as actorName has them, a runtime without its model.
export function producerName(actor: string): string {
  if (actor.startsWith('human:') || actor.startsWith('process:')) return actorName(actor)
  return actorName(actor.split('/')[0])
}

export function isPerson(actor: string | undefined): boolean {
  return actor?.startsWith('human:') ?? false
}

const tiers: Record<string, MessageKey> = {
  unverified: 'wiki.tier.unverified',
  'machine-confirmed': 'wiki.tier.machine',
  'human-reviewed': 'wiki.tier.human',
}

export function tierName(t: T, tier: string): string {
  return tiers[tier] ? t(tiers[tier]) : tier
}

// vouched says a person stands behind the page as it is now.
export function vouched(page: WikiPageInfo): boolean {
  return page.vouched_at !== undefined
}

const changes: Record<string, MessageKey> = {
  Creation: 'wiki.change.creation',
  Update: 'wiki.change.update',
  Deprecation: 'wiki.change.deprecation',
  Rename: 'wiki.change.rename',
  Verification: 'wiki.change.verification',
  Revert: 'wiki.change.revert',
  Rejection: 'wiki.change.rejection',
  Initialization: 'wiki.change.initialization',
}

export function changeName(t: T, kind: string): string {
  return changes[kind] ? t(changes[kind]) : kind
}

// The subjects the hub gives the wiki's commits (internal/hub, internal/
// wiki), which git keeps in English, as the UI says them.
const subjects: [RegExp, MessageKey][] = [
  [/^Confirmed: (.*)$/, 'wiki.subject.confirmed'],
  [/^Accepted: (.*)$/, 'wiki.subject.accepted'],
  [/^Turned down: (.*)$/, 'wiki.subject.turnedDown'],
  [/^Resident: (.*)$/, 'wiki.subject.resident'],
  [/^No longer resident: (.*)$/, 'wiki.subject.notResident'],
  [/^Handed over to \S+: (.*)$/, 'wiki.subject.handedOver'],
  // A turn's change to the library names the turn's project by its folder.
  [/^(.*) of ([a-z0-9]+(?:-[a-z0-9]+)*) in topic #(\d+)$/, 'wiki.subject.libraryTurn'],
  [/^(.*) in topic #(\d+)$/, 'wiki.subject.turn'],
  [/^Record changes (?:made outside Veyloom|found when Veyloom started)$/, 'wiki.subject.outside'],
  [/^Undo \S+: (.*)$/, 'wiki.subject.undo'],
  // A skill's trial and where skills come from (docs/design.md 5.15).
  [/^Keep (\S+) after its trial$/, 'wiki.subject.trialKept'],
  [/^Roll back (\S+) to \S+$/, 'wiki.subject.rolledBack'],
  [/^Imported the skill (\S+) from .*$/, 'wiki.subject.imported'],
  // The memories a person keeps (docs/design.md 5.16).
  [/^Changed the project memory$/, 'wiki.subject.projectMemory'],
  [/^Changed the personal memory$/, 'wiki.subject.personalMemory'],
]

// subjectText is a commit's subject in the UI's words; one it does not
// know stays as written.
export function subjectText(t: T, subject: string): string {
  for (const [pattern, key] of subjects) {
    const match = pattern.exec(subject)
    if (!match) continue
    if (key === 'wiki.subject.turn') return t(key, { who: match[1], n: match[2] })
    if (key === 'wiki.subject.libraryTurn') return t(key, { who: match[1], project: match[2], n: match[3] })
    if (key === 'wiki.subject.undo') return t(key, { what: subjectText(t, match[1]) })
    return t(key, { title: match[1] ?? '' })
  }
  return subject
}

// commitNote is what a commit's subject says that the lines for its pages
// do not: a skill handed to another team, a gone project's skills left to
// nobody, the indexes written afresh. Empty for the rest.
export function commitNote(t: T, subject: string, teams: WikiTeam[] = []): string {
  const handed = /^Handed over to (\S+): /.exec(subject)
  if (handed) return t('wiki.note.handedOver', { team: teams.find((team) => team.slug === handed[1])?.name ?? handed[1] })
  const orphaned = /^Left without a team: the project (\S+) is gone$/.exec(subject)
  if (orphaned) return t('wiki.note.orphaned', { team: orphaned[1] })
  return subject === 'Regenerate the indexes' ? t('wiki.note.indexes') : ''
}

// reasonOf is why a person or an agent made a change, as the log line has
// it after them: "undid <sha> (<subject>) by <actor>: why", "[<title>](<path>)
// rolled back to <sha> by <actor>: why", and for a page changed or
// deprecated with a reason "[<title>](<path>) by <actor>: why". What
// Veyloom notes itself (process:…) the commit's subject says already.
export function reasonOf(text: string): string | undefined {
  return /(?:^undid \S+ \(.*\)| rolled back to \S+|^\[.*\]\([^)\s]*\)(?: was \S+)?) by (?!process:)\S+?: (.+)$/.exec(text)?.[1]
}

// relationNames head each kind of relation a page has (docs/design.md
// 5.17), on the graph's card and under a page.
export const relationNames: Record<RelationKey, MessageKey> = {
  linksTo: 'wiki.rel.linksTo',
  linkedFrom: 'wiki.rel.linkedFrom',
  supersedes: 'wiki.rel.supersedes',
  supersededBy: 'wiki.rel.supersededBy',
  restsOn: 'wiki.rel.restsOn',
  restedOnBy: 'wiki.rel.restedOnBy',
  names: 'wiki.rel.names',
  namedBy: 'wiki.rel.namedBy',
  cameFrom: 'wiki.rel.cameFrom',
  gave: 'wiki.rel.gave',
  within: 'wiki.rel.within',
  holds: 'wiki.rel.holds',
}
