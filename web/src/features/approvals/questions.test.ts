import { describe, expect, it } from 'vitest'
import type { Question } from '@/api/types'
import { approval } from '@/test/fixtures'
import { approvalCommand } from './describe'
import { OTHER, answerOf, canWrite, questionsOf } from './questions'

const single: Question = { id: '1', question: 'Which database?', options: [{ label: 'Postgres' }, { label: 'SQLite' }], other: true }
const multi: Question = { id: '2', question: 'Which checks?', options: [{ label: 'lint' }, { label: 'test' }], multiSelect: true, other: true }
const open: Question = { id: '3', question: 'Your token?', secret: true }

describe('questions', () => {
  it('turns what was picked and written into answers', () => {
    expect(answerOf(single, [], '')).toEqual([])
    expect(answerOf(single, ['SQLite'], 'ignored')).toEqual(['SQLite'])
    expect(answerOf(single, [OTHER], '  DuckDB ')).toEqual(['DuckDB'])
    expect(answerOf(single, [OTHER], '  ')).toEqual([])
    expect(answerOf(multi, ['lint', OTHER, 'test'], 'vet')).toEqual(['lint', 'test', 'vet'])
    expect(answerOf(open, [], ' hunter2 ')).toEqual(['hunter2'])
  })

  it("knows when an answer of one's own is possible", () => {
    expect(canWrite(single)).toBe(true)
    expect(canWrite({ ...single, other: false })).toBe(false)
    expect(canWrite(open)).toBe(true)
  })

  it('reads the questions of a question approval, and none of any other', () => {
    const asked = approval('q1', { kind: 'question', tool: 'AskUserQuestion', input: { questions: [single, multi] } })
    expect(questionsOf(asked)).toEqual([single, multi])
    expect(questionsOf({ ...asked, input: JSON.stringify({ questions: [open] }) })).toEqual([open])
    expect(questionsOf({ ...asked, input: 'not json' })).toEqual([])
    expect(questionsOf(approval('a1'))).toEqual([])
    expect(approvalCommand(asked)).toBe('Which database?（还有 1 个问题）')
    expect(approvalCommand({ ...asked, input: { questions: [open] } })).toBe('Your token?')
  })
})
