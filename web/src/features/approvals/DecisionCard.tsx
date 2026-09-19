import { type ReactNode, useId, useRef, useState } from 'react'
import { ApiError } from '@/api/client'
import { useDecideApproval } from '@/api/approvals'
import { StatusDot, type StatusTone } from '@/components/shared/status-dot'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { MessageKey } from '@/i18n/zh-CN'
import { useCurrentUser } from '@/lib/currentUser'
import { formatTime } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import type { ApprovalCardProps } from './ApprovalCard'
import { decisionNote } from './describe'

type Status = 'pending' | 'allowed' | 'denied' | 'expired' | 'cancelled'

// The words a yes-or-no card uses, by message key.
export interface DecisionWords {
  label: MessageKey
  heads: Record<Status, MessageKey>
  yes: MessageKey
  no: MessageKey
  yesBy: MessageKey
  noBy: MessageKey
  // Absent when what is asked cannot carry a note back.
  notePlaceholder?: MessageKey
}

const tones: Record<Status, StatusTone> = { pending: 'wait', allowed: 'ok', denied: 'idle', expired: 'idle', cancelled: 'idle' }

// DecisionCard is a request a person answers yes or no, drawn in words of
// its own rather than as a permission to run something: what is asked (its
// children), the two answers and maybe a note; once settled, who answered
// and when (docs/design.md 4.6).
export function DecisionCard({ approval, memberName, names, words, children }: ApprovalCardProps & { words: DecisionWords; children: ReactNode }) {
  const t = useT()
  const user = useCurrentUser()
  const decide = useDecideApproval()
  const noteId = useId()
  const note = useRef<HTMLInputElement>(null)
  const [error, setError] = useState<string>()
  const status: Status = approval.status in tones ? (approval.status as Status) : 'cancelled'
  const pending = status === 'pending'
  const who = approval.decided_by ? (names.get(approval.decided_by) ?? t('approval.someone')) : undefined

  function send(allow: boolean) {
    if (!user) return
    decide.mutate(
      { id: approval.id, user_id: user.id, allow, message: note.current?.value.trim() ?? '' },
      { onError: (err) => setError(err instanceof ApiError && err.status === 409 ? t('approval.raced') : err.message) },
    )
  }

  return (
    <div
      role="group"
      aria-label={t(words.label)}
      className={cn(
        'mt-2.5 flex flex-col gap-2.5 rounded-[10px] bg-muted px-3.5 py-3 text-foreground',
        (status === 'expired' || status === 'cancelled') && 'opacity-55',
      )}
    >
      <div className="flex items-center gap-2 text-[0.8125rem] text-muted-foreground">
        <StatusDot tone={tones[status]} />
        {memberName ? <b className="font-medium text-foreground">{memberName}</b> : null}
        <span className={cn(pending && 'font-medium text-foreground')}>{t(words.heads[status])}</span>
      </div>
      {children}
      {pending ? (
        <>
          {words.notePlaceholder ? (
            <>
              <Label htmlFor={noteId} className="sr-only">
                {t('approval.note')}
              </Label>
              <Input
                id={noteId}
                ref={note}
                placeholder={t(words.notePlaceholder)}
                autoComplete="off"
                spellCheck={false}
                className="h-7 bg-background text-[0.78125rem]"
              />
            </>
          ) : null}
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
              {t(words.no)}
            </Button>
            <Button size="sm" onClick={() => send(true)} disabled={!user || decide.isPending} aria-busy={decide.isPending}>
              {t(words.yes)}
            </Button>
          </div>
        </>
      ) : (
        <div className="text-[0.78125rem] text-subtle">
          {who === undefined ? t('approval.nobody') : t(status === 'allowed' ? words.yesBy : words.noBy, { who })}
          {approval.decided_at ? ` · ${formatTime(approval.decided_at)}` : ''}
          {approval.message ? ` · “${decisionNote(approval.message)}”` : ''}
        </div>
      )}
    </div>
  )
}
