import { useId, useRef, useState } from 'react'
import { useDecideApproval } from '@/api/approvals'
import { errorText } from '@/api/errorText'
import type { AllowScope, Approval } from '@/api/types'
import { Confirmation, ConfirmationAction, ConfirmationActions, ConfirmationRequest } from '@/components/ai-elements/confirmation'
import { StatusDot } from '@/components/shared/status-dot'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useCurrentUser } from '@/lib/currentUser'
import { useT } from '@/lib/i18n'
import { AllowButton } from './AllowButton'
import { approvalCommand, approvalPurpose } from './describe'
import { SettledApproval } from './SettledApproval'

export interface ApprovalCardProps {
  approval: Approval
  // Who asked; omitted when the card sits under the agent's own turn.
  memberName?: string
  // Display names by user id, for who decided.
  names: Map<string, string>
}

// A permission request as AI Elements draws a tool confirmation: what the
// agent wants to run, what it said that is for, and the buttons that
// answer it (docs/webui.md §4.5). Allowing it is a split button whose menu
// lets more through with it (AllowButton). Once settled it is a line of
// its own (SettledApproval).
export function ApprovalCard(props: ApprovalCardProps) {
  if (props.approval.status !== 'pending') return <SettledApproval {...props} />
  return <PendingApproval {...props} />
}

function PendingApproval({ approval, memberName }: ApprovalCardProps) {
  const user = useCurrentUser()
  const decide = useDecideApproval()
  const noteId = useId()
  const note = useRef<HTMLInputElement>(null)
  const [error, setError] = useState<string>()
  const t = useT()
  const purpose = approvalPurpose(approval)

  function answer(allow: boolean, scope: AllowScope = 'once') {
    if (!user) return
    decide.mutate(
      { id: approval.id, user_id: user.id, allow, ...(scope !== 'once' ? { scope } : {}), message: note.current?.value.trim() ?? '' },
      { onError: (err) => setError(errorText(err, { 409: t('approval.raced') })) },
    )
  }

  return (
    <Confirmation
      role="group"
      aria-label={t('approval.requests')}
      state="approval-requested"
      approval={{ id: approval.id }}
      className="mt-2.5 gap-2 rounded-[10px] border-0 bg-muted px-3.5 py-3 text-foreground"
    >
      <div className="flex items-center gap-2 text-[0.8125rem] text-muted-foreground">
        <StatusDot tone="wait" />
        {memberName ? <b className="font-medium text-foreground">{memberName}</b> : null}
        <b className="font-medium text-foreground">{t('approval.requests')}</b>
        {purpose ? <span className="min-w-0 truncate">{purpose}</span> : null}
      </div>
      <div className="font-mono text-[0.8125rem] break-all whitespace-pre-wrap text-foreground" translate="no">
        {approvalCommand(approval)}
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
        <div className="mt-1 flex flex-wrap items-center gap-1.5">
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
            <AllowButton approval={approval} disabled={!user || decide.isPending} busy={decide.isPending} onAllow={(scope) => answer(true, scope)} />
          </ConfirmationActions>
        </div>
      </ConfirmationRequest>
    </Confirmation>
  )
}
