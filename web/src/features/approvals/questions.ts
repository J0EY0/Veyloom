import type { Approval, Question, QuestionSet } from '@/api/types'

// OTHER is the choice that stands for an answer of one's own among the
// options a person picks from; it never reaches the runtime.
export const OTHER = '__veyloom_other__'

// questionsOf reads what a question approval asks; any other kind asks none.
export function questionsOf(approval: Pick<Approval, 'kind' | 'input'>): Question[] {
  if (approval.kind !== 'question') return []
  let input = approval.input
  if (typeof input === 'string') {
    try {
      input = JSON.parse(input) as unknown
    } catch {
      return []
    }
  }
  const questions = (input as Partial<QuestionSet> | null)?.questions
  return Array.isArray(questions) ? questions : []
}

// canWrite says whether a question takes an answer of one's own at all.
export function canWrite(question: Question): boolean {
  return Boolean(question.other) || !question.options?.length
}

// answerOf turns what a person did with one question, the options picked
// and the text written, into its answer: empty until it is answered. Text
// of several lines goes back as written, all but blank.
export function answerOf(question: Question, picked: string[], written: string): string[] {
  const text = question.multiline && written.trim() ? written : written.trim()
  if (!question.options?.length) return text ? [text] : []
  const chosen = question.multiSelect ? picked : picked.slice(0, 1)
  const answer = chosen.filter((choice) => choice !== OTHER)
  if (chosen.includes(OTHER) && text) answer.push(text)
  return answer
}
