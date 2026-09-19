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
