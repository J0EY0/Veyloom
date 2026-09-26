import type { Approval } from '@/api/types'
import { describeTool } from '@/features/turns/activity'
import { t } from '@/lib/i18n'
import { formOf, linkOf } from './forms'
import { confirmationOf, planOf, planTitle } from './plans'
import { questionsOf } from './questions'

// approvalCommand says what an approval asks for in one line: the shell
// command itself, the question asked, what a form or a link is for, or
// the tool and its input.
export function approvalCommand(approval: Pick<Approval, 'kind' | 'tool' | 'input'>): string {
  const form = formOf(approval) ?? linkOf(approval)
  if (form) return form.message || form.server
  const plan = planOf(approval)
  if (plan !== undefined) return t('tool.plan', { title: planTitle(plan) || t('plan.untitled') })
  const confirmation = confirmationOf(approval)
  if (confirmation) return t('tool.confirm', { text: [confirmation.title, confirmation.message].filter(Boolean).join(' — ') })
  const questions = questionsOf(approval)
  if (questions.length > 0) {
    const [first] = questions
    return questions.length > 1 ? t('question.summary', { first: first.question, n: questions.length - 1 }) : first.question
  }
  const input = typeof approval.input === 'string' ? approval.input : JSON.stringify(approval.input ?? '')
  return describeTool(approval.tool, input)
}

// approvalPurpose is what the runtime said a command is for, when it said
// (Claude Code's Bash sends a description): shown beside the command so a
// person need not work it out.
export function approvalPurpose(approval: Pick<Approval, 'input'>): string | undefined {
  let input = approval.input
  if (typeof input === 'string') {
    try {
      input = JSON.parse(input)
    } catch {
      return undefined
    }
  }
  const description = (input as { description?: unknown } | null)?.description
  return typeof description === 'string' && description.trim() !== '' ? description.trim() : undefined
}

// decisionNote is the note that came with a decision, in the reader's
// language when the hub wrote it rather than a person: the turn ending
// first, the runtime taking the request back, nobody deciding in time.
export function decisionNote(message: string): string {
  if (message === 'the turn ended before a decision') return t('approval.reason.turnEnded')
  if (message === 'the runtime took the request back') return t('approval.reason.withdrawn')
  const timeout = /^nobody decided within (.+)$/.exec(message)
  if (timeout) return t('approval.reason.timeout', { after: timeout[1] })
  return message
}

// similarScope says what allowing a request "and the like of it" takes in
// for the rest of the turn, as its runtime offered: commands starting the
// same way, a command again, files under a folder, file changes, folders;
// undefined when it offered nothing.
export function similarScope(approval: Pick<Approval, 'tool' | 'similar'>): string | undefined {
  return scopeTerms(approval, 'similar')?.words
}

// ScopeTerms is what an allow takes in, both ways: in words, and as
// command patterns (go test *) when it is only commands.
export interface ScopeTerms {
  words: string
  code?: string
}

// scopeTerms says what allowing a request with the like of it takes in:
// for the rest of the turn ('similar'), everything the runtime offered;
// from now on ('always'), what of it can be kept for the member, Claude
// Code's rules and Codex's command prefix (docs/design.md 4.6). Undefined
// when there is nothing to take in.
export function scopeTerms(approval: Pick<Approval, 'tool' | 'similar'>, scope: 'similar' | 'always'): ScopeTerms | undefined {
  const offer = approval.similar
  if (!offer) return undefined
  const rules = [...(offer.rules ?? []), ...(offer.prefix && offer.prefix.length > 0 ? [prefixRule(offer.prefix)] : [])]
  const words = rules.map(ruleText)
  const codes = rules.map(commandPattern)
  if (scope === 'similar') {
    if (offer.mode) words.push(offer.mode === 'acceptEdits' ? t('approval.similar.edits') : t('approval.similar.mode', { mode: offer.mode }))
    if (offer.dirs && offer.dirs.length > 0) words.push(t('approval.similar.dirs', { dirs: offer.dirs.join('、') }))
    // Commands starting the same way take in the same command again.
    if (offer.same && !offer.prefix?.length) words.push(t(approval.tool === 'fileChange' ? 'approval.similar.sameFiles' : 'approval.similar.sameCommand'))
  }
  if (words.length === 0) return undefined
  const code = words.length === codes.length && codes.every((c) => c !== undefined) ? codes.join(', ') : undefined
  return { words: words.join('、'), code }
}

// prefixRule is a Codex command prefix as the rule Veyloom keeps: its
// words as a JSON array.
function prefixRule(words: string[]): string {
  return JSON.stringify(words)
}

// commandPattern is a rule allowing commands as the pattern it allows: go
// test * for Bash(go test:*) or ["go","test"], make test for Bash(make
// test). Undefined for a rule about anything else.
export function commandPattern(rule: string): string | undefined {
  const prefix = /^Bash\((.+?)(?::\*| \*)\)$/.exec(rule)
  if (prefix) return `${prefix[1]} *`
  const command = /^Bash\((.+)\)$/.exec(rule)
  if (command) return command[1]
  const words = prefixWords(rule)
  return words ? `${words.join(' ')} *` : undefined
}

// prefixWords reads a Codex prefix kept as a JSON array of words.
function prefixWords(rule: string): string[] | undefined {
  if (!rule.startsWith('[')) return undefined
  try {
    const words: unknown = JSON.parse(rule)
    return Array.isArray(words) && words.length > 0 && words.every((w) => typeof w === 'string') ? words : undefined
  } catch {
    return undefined
  }
}

// ruleText says a rule in words: one of Claude Code's permission rules,
// Bash(go test *) or its older Bash(go test:*), Bash(make test),
// Read(//etc/**); or a Codex command prefix.
export function ruleText(rule: string): string {
  const prefix = /^Bash\((.+?)(?::\*| \*)\)$/.exec(rule)
  if (prefix) return t('approval.similar.prefix', { command: prefix[1] })
  const command = /^Bash\((.+)\)$/.exec(rule)
  if (command) return t('approval.similar.command', { command: command[1] })
  const under = /^(Read|Edit|Write)\(\/(\/.*?)\/\*\*\)$/.exec(rule)
  if (under) return t(under[1] === 'Read' ? 'approval.similar.readUnder' : 'approval.similar.editUnder', { dir: under[2] })
  const domain = /^WebFetch\(domain:(.+)\)$/.exec(rule)
  if (domain) return t('approval.similar.domain', { domain: domain[1] })
  const words = prefixWords(rule)
  if (words) return t('approval.similar.prefix', { command: words.join(' ') })
  return rule
}
