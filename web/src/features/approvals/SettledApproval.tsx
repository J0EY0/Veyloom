import { ChevronRightIcon, ShieldCheckIcon } from 'lucide-react'
import type { Approval } from '@/api/types'
import { StatusDot, type StatusTone } from '@/components/shared/status-dot'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import type { MessageKey } from '@/i18n/zh-CN'
import { formatTime } from '@/lib/format'
import { t, useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { approvalCommand, approvalPurpose, decisionNote, scopeTerms } from './describe'

// Reviewers a runtime brings of its own, by the code the hub records.
const reviewers: Record<string, MessageKey> = {
  codex_auto_review: 'approval.reviewer.codexAutoReview',
}

// What let a request through for a person without asking them: the turn
// they trusted, or a rule they allowed always (docs/design.md 4.6).
const standing: Record<string, MessageKey> = {
  turn: 'approval.reviewer.turn',
  rule: 'approval.reviewer.rule',
}

const risks: Record<string, MessageKey> = {
  low: 'approval.risk.low',
  medium: 'approval.risk.medium',
  high: 'approval.risk.high',
  critical: 'approval.risk.critical',
}

const outcome = {
  allowed: { tone: 'ok', key: 'approval.ran' },
  denied: { tone: 'fail', key: 'approval.denied' },
  expired: { tone: 'idle', key: 'approval.expired' },
  cancelled: { tone: 'idle', key: 'approval.cancelled' },
} as const satisfies Record<string, { tone: StatusTone; key: MessageKey }>

export interface SettledApprovalProps {
  approval: Approval
  // Who asked; omitted when the line sits under the agent's own turn.
  memberName?: string
  // Display names by user id, for who decided.
  names: Map<string, string>
}

// SettledApproval is a permission request once settled (docs/webui.md
// §4.5): one line with how it went, the command and who decided when, so a
// topic where an agent asked many times still reads as its work. The line
// opens onto the whole command, what it was for, how far the allow went
// and the note that came with the decision. When the runtime's own
// reviewer decided, the line names it and the risk it saw; when a trusted
// turn or a rule let it through, a shield says so (docs/design.md 4.6).
// What nobody decided is dimmed.
export function SettledApproval({ approval, memberName, names }: SettledApprovalProps) {
  const t = useT()
  const command = approvalCommand(approval)
  const purpose = approvalPurpose(approval)
  const result = approval.status in outcome ? outcome[approval.status as keyof typeof outcome] : undefined
  const dim = approval.status === 'expired' || approval.status === 'cancelled'
  const shield = approval.reviewer ? standing[approval.reviewer] : undefined
  const reviewer = approval.reviewer && !shield ? (reviewers[approval.reviewer] ? t(reviewers[approval.reviewer]) : approval.reviewer) : undefined
  const risk = findingOf(approval.answer, 'risk')
  const who = reviewer ?? (approval.decided_by ? (names.get(approval.decided_by) ?? t('approval.someone')) : undefined)
  const decided = shield
    ? t(shield)
    : who === undefined
      ? t('approval.nobody')
      : approval.status === 'allowed' || approval.status === 'denied'
        ? t(approval.status === 'allowed' ? 'approval.allowedBy' : 'approval.deniedBy', { who })
        : t('approval.reviewUnfinished', { who })
  const by = [decided, reviewer && risk ? (risks[risk] ? t(risks[risk]) : risk) : '', approval.decided_at ? formatTime(approval.decided_at) : '']
    .filter(Boolean)
    .join(' · ')
  const scope = scopeLine(approval)

  return (
    <Collapsible className={cn('mt-1.5', dim && 'opacity-55')}>
      <CollapsibleTrigger className="group/settled flex w-full min-w-0 items-center gap-2 rounded-md py-1 text-left text-[0.78125rem] text-muted-foreground transition-colors hover:text-foreground">
        <StatusDot tone={result?.tone ?? 'idle'} />
        {memberName ? <b className="flex-none font-medium text-foreground">{memberName}</b> : null}
        <span className="flex-none">{result ? t(result.key) : approval.status}</span>
        <code className="min-w-0 flex-1 truncate font-mono text-foreground/85" translate="no">
          {command.split('\n')[0]}
        </code>
        {/* The command comes first: who and when give way to it. */}
        <span className="flex max-w-[40%] flex-none items-center gap-1 text-subtle">
          {shield ? <ShieldCheckIcon aria-hidden="true" className="size-3 flex-none" /> : null}
          <span className="min-w-0 truncate">{by}</span>
        </span>
        <ChevronRightIcon aria-hidden="true" className="size-3.5 flex-none text-subtle transition-transform group-data-[state=open]/settled:rotate-90" />
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="mt-1 mb-1 flex flex-col gap-1.5 rounded-[10px] bg-muted px-3.5 py-2.5 text-[0.78125rem] text-muted-foreground">
          <div className="font-mono text-[0.8125rem] break-all whitespace-pre-wrap text-foreground" translate="no">
            {command}
          </div>
          {purpose ? <div>{purpose}</div> : null}
          {scope ? <div>{scope}</div> : null}
          {approval.message ? <div>“{decisionNote(approval.message)}”</div> : null}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}

// scopeLine says how far a person's allow went beyond the request itself:
// the like of it for the rest of the turn or from now on, or the rest of
// the turn trusted.
function scopeLine(approval: Approval): string | undefined {
  switch (approval.scope) {
    case 'similar':
    case 'always': {
      const terms = scopeTerms(approval, approval.scope)
      return terms
        ? t(approval.scope === 'similar' ? 'approval.scopeDone.similar' : 'approval.scopeDone.always', { scope: terms.code ?? terms.words })
        : undefined
    }
    case 'turn':
      return t('approval.scopeDone.turn')
  }
  return undefined
}

// findingOf reads one of a reviewer's findings from an approval's answer.
function findingOf(answer: unknown, key: string): string | undefined {
  if (typeof answer !== 'object' || answer === null) return undefined
  const value = (answer as Record<string, unknown>)[key]
  return typeof value === 'string' && value !== '' ? value : undefined
}
