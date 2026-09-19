import { useId, useState } from 'react'
import { ApiError } from '@/api/client'
import { useDecideApproval } from '@/api/approvals'
import type { Approval, Question, QuestionAnswers } from '@/api/types'
import { StatusDot, type StatusTone } from '@/components/shared/status-dot'
import { Button } from '@/components/ui/button'
import type { MessageKey } from '@/i18n/zh-CN'
import { useCurrentUser } from '@/lib/currentUser'
import { formatTime } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import type { ApprovalCardProps } from './ApprovalCard'
import { QuestionField } from './QuestionField'
import { answerOf, questionsOf } from './questions'
import { decisionNote } from './describe'

const heads = {
  pending: { tone: 'wait', key: 'question.asking' },
  allowed: { tone: 'ok', key: 'question.answered' },
  denied: { tone: 'idle', key: 'question.declined' },
  expired: { tone: 'idle', key: 'question.expired' },
  cancelled: { tone: 'idle', key: 'question.cancelled' },
} as const satisfies Record<string, { tone: StatusTone; key: MessageKey }>

// What an agent asks a person mid-turn, whichever runtime asks it
// (docs/design.md 4.6): each question with its options to pick from or a
// line to write in, sent back as answers; or declined. Once settled it
// shows what was answered, by whom and when.
export function QuestionCard({ approval, memberName, names }: ApprovalCardProps) {
  const t = useT()
  const user = useCurrentUser()
  const decide = useDecideApproval()
  const idPrefix = useId()
  const [picked, setPicked] = useState<Record<string, string[]>>({})
  const [written, setWritten] = useState<Record<string, string>>({})
  const [error, setError] = useState<string>()
  const questions = questionsOf(approval)
  const pending = approval.status === 'pending'
  const head = heads[approval.status as keyof typeof heads] ?? heads.cancelled
  const writtenOf = (q: Question) => written[q.id] ?? q.default ?? ''
  const answers = Object.fromEntries(questions.map((q) => [q.id, answerOf(q, picked[q.id] ?? [], writtenOf(q))]))
  const complete = questions.length > 0 && questions.every((q) => answers[q.id].length > 0)

  function send(allow: boolean) {
    if (!user) return
    decide.mutate(
      { id: approval.id, user_id: user.id, allow, message: '', ...(allow ? { answer: { answers } } : {}) },
      { onError: (err) => setError(err instanceof ApiError && err.status === 409 ? t('approval.raced') : err.message) },
    )
  }

  return (
    <div
      role="group"
      aria-label={t('question.label')}
      className={cn(
        'mt-2.5 flex flex-col gap-3 rounded-[10px] bg-muted px-3.5 py-3 text-foreground',
        (approval.status === 'expired' || approval.status === 'cancelled') && 'opacity-55',
      )}
    >
      <div className="flex items-center gap-2 text-[0.8125rem] text-muted-foreground">
        <StatusDot tone={head.tone} />
        {memberName ? <b className="font-medium text-foreground">{memberName}</b> : null}
        <span className={cn(pending && 'font-medium text-foreground')}>{t(head.key)}</span>
      </div>
      {pending ? (
        <>
          {questions.map((q, index) => (
            <QuestionField
              key={q.id}
              question={q}
              idPrefix={`${idPrefix}-${index}`}
              picked={picked[q.id] ?? []}
              written={writtenOf(q)}
              onPick={(next) => setPicked((old) => ({ ...old, [q.id]: next }))}
              onWrite={(next) => setWritten((old) => ({ ...old, [q.id]: next }))}
              disabled={decide.isPending}
            />
          ))}
          <div className="flex items-center gap-1.5">
            <span className="text-xs text-subtle">
              {error ? (
                <span role="alert" className="text-status-fail">
                  {error}
                </span>
              ) : user ? null : (
                t('common.identifyingShort')
              )}
            </span>
            <span className="grow" />
            <Button variant="ghost" size="sm" onClick={() => send(false)} disabled={!user || decide.isPending}>
              {t('question.decline')}
            </Button>
            <Button size="sm" onClick={() => send(true)} disabled={!user || !complete || decide.isPending} aria-busy={decide.isPending}>
              {t('question.submit')}
            </Button>
          </div>
        </>
      ) : (
        <Settled approval={approval} questions={questions} names={names} />
      )}
    </div>
  )
}

// Settled shows a question once it is over: the answers given, or the
// questions left unanswered, and who settled it when.
function Settled({ approval, questions, names }: { approval: Approval; questions: Question[]; names: Map<string, string> }) {
  const t = useT()
  const given = answersOf(approval.answer)
  const who = approval.decided_by ? (names.get(approval.decided_by) ?? t('approval.someone')) : undefined
  const line =
    who === undefined ? t('question.unanswered') : approval.status === 'allowed' ? t('question.answeredBy', { who }) : t('question.declinedBy', { who })
  return (
    <>
      <dl className="grid gap-2 text-[0.8125rem]">
        {questions.map((q) => (
          <div key={q.id} className="grid gap-0.5">
            <dt className="text-muted-foreground">{q.question}</dt>
            {approval.status === 'allowed' ? (
              <dd className="break-words whitespace-pre-wrap text-foreground">{(given[q.id] ?? []).join('、') || '—'}</dd>
            ) : null}
          </div>
        ))}
      </dl>
      <div className="text-[0.78125rem] text-subtle">
        {line}
        {approval.decided_at ? ` · ${formatTime(approval.decided_at)}` : ''}
        {approval.message ? ` · “${decisionNote(approval.message)}”` : ''}
      </div>
    </>
  )
}

function answersOf(answer: unknown): Record<string, string[]> {
  const answers = (answer as Partial<QuestionAnswers> | null | undefined)?.answers
  return answers && typeof answers === 'object' ? answers : {}
}
