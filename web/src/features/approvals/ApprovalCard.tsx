import { useId, useRef, useState } from 'react'
import { ApiError } from '@/api/client'
import { useDecideApproval } from '@/api/approvals'
import type { Approval } from '@/api/types'
import {
  Confirmation,
  ConfirmationAccepted,
  ConfirmationAction,
  ConfirmationActions,
  ConfirmationRejected,
  ConfirmationRequest,
} from '@/components/ai-elements/confirmation'
import { StatusDot, type StatusTone } from '@/components/shared/status-dot'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useCurrentUser } from '@/lib/currentUser'
import { formatTime } from '@/lib/format'
import { cn } from '@/lib/utils'
import { approvalCommand } from './describe'
import { useT } from '@/lib/i18n'
import type { MessageKey } from '@/i18n/zh-CN'

export interface ApprovalCardProps {
  approval: Approval
  // Who asked; omitted when the card sits under the agent's own turn.
  memberName?: string
  // Display names by user id, for who decided.
  names: Map<string, string>
}

const outcome = {
  allowed: { tone: 'ok', key: 'approval.ran' },
  denied: { tone: 'fail', key: 'approval.denied' },
  expired: { tone: 'idle', key: 'approval.expired' },
  cancelled: { tone: 'idle', key: 'approval.cancelled' },
} as const satisfies Record<string, { tone: StatusTone; key: MessageKey }>

// A permission request as AI Elements draws a tool confirmation: what the
// agent wants to run, and the two buttons that answer it. Once settled it
// says who decided and when (docs/webui.md §4.5).
export function ApprovalCard({ approval, memberName, names }: ApprovalCardProps) {
  const user = useCurrentUser()
  const decide = useDecideApproval()
  const noteId = useId()
  const note = useRef<HTMLInputElement>(null)
  const [error, setError] = useState<string>()
  const t = useT()
  const command = approvalCommand(approval)
  const pending = approval.status === 'pending'
  const result = approval.status in outcome ? outcome[approval.status as keyof typeof outcome] : undefined
  const dim = approval.status === 'expired' || approval.status === 'cancelled'

  function answer(allow: boolean) {
    if (!user) return
    decide.mutate(
      { id: approval.id, user_id: user.id, allow, message: note.current?.value.trim() ?? '' },
      {
        onError: (err) => {
          setError(err instanceof ApiError && err.status === 409 ? t('approval.raced') : err.message)
        },
      },
    )
  }

  const decided = (
    <div className="text-[0.78125rem] text-subtle">
      {approval.decided_by
        ? t(approval.status === 'allowed' ? 'approval.allowedBy' : 'approval.deniedBy', { who: names.get(approval.decided_by) ?? t('approval.someone') })
        : t('approval.nobody')}
      {approval.decided_at ? ` · ${formatTime(approval.decided_at)}` : ''}
      {approval.message ? ` · “${approval.message}”` : ''}
    </div>
  )

  return (
    <Confirmation
      role="group"
      aria-label={t('approval.requests')}
      state={pending ? 'approval-requested' : approval.status === 'allowed' ? 'output-available' : 'output-denied'}
      approval={pending ? { id: approval.id } : { id: approval.id, approved: approval.status === 'allowed', reason: approval.message }}
      className={cn('mt-2.5 gap-2 rounded-[10px] border-0 bg-muted px-3.5 py-3 text-foreground', dim && 'opacity-55')}
    >
      <div className="flex items-center gap-2 text-[0.8125rem] text-muted-foreground">
        <StatusDot tone={pending ? 'wait' : (result?.tone ?? 'idle')} />
        {memberName ? <b className="font-medium text-foreground">{memberName}</b> : null}
        {pending ? <b className="font-medium text-foreground">{t('approval.requests')}</b> : <span>{result ? t(result.key) : approval.status}</span>}
      </div>
      <div className="font-mono text-[0.8125rem] break-all whitespace-pre-wrap text-foreground" translate="no">
        {command}
      </div>
      <ConfirmationRequest>
        <Label htmlFor={noteId} className="sr-only">
          {t('approval.note')}
        </Label>
        <Input
          id={noteId}
          ref={note}
          placeholder={t('approval.notePlaceholder')}
          autoComplete="off"
          spellCheck={false}
          className="mt-0.5 h-7 bg-background text-[0.78125rem]"
        />
        <div className="mt-1 flex items-center gap-1.5">
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
          <ConfirmationActions className="self-auto">
            <ConfirmationAction variant="ghost" size="sm" onClick={() => answer(false)} disabled={!user || decide.isPending}>
              {t('common.deny')}
            </ConfirmationAction>
            <ConfirmationAction size="sm" onClick={() => answer(true)} disabled={!user || decide.isPending} aria-busy={decide.isPending}>
              {t('common.allow')}
            </ConfirmationAction>
          </ConfirmationActions>
        </div>
      </ConfirmationRequest>
      <ConfirmationAccepted>{decided}</ConfirmationAccepted>
      <ConfirmationRejected>{decided}</ConfirmationRejected>
    </Confirmation>
  )
}
