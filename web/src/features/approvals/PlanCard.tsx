import { ChatMarkdown } from '@/components/shared/chat-markdown'
import { useT } from '@/lib/i18n'
import type { ApprovalCardProps } from './ApprovalCard'
import { DecisionCard, type DecisionWords } from './DecisionCard'
import { planOf } from './plans'

const words: DecisionWords = {
  label: 'plan.label',
  heads: { pending: 'plan.asking', allowed: 'plan.approved', denied: 'plan.rejected', expired: 'plan.expired', cancelled: 'plan.cancelled' },
  yes: 'plan.approve',
  no: 'plan.reject',
  yesBy: 'plan.approvedBy',
  noBy: 'plan.rejectedBy',
  notePlaceholder: 'plan.notePlaceholder',
}

const noNames = new Map<string, string>()

// A plan Claude Code made in plan mode and puts up for approval (docs/
// design.md 4.6), shown as the markdown it is. Approved, or sent back with
// a note the agent reads.
export function PlanCard(props: ApprovalCardProps) {
  const t = useT()
  const plan = planOf(props.approval) ?? ''
  return (
    <DecisionCard {...props} words={words}>
      <div className="max-h-80 overflow-y-auto rounded-md bg-background px-3 py-2">
        {plan.trim() ? (
          <ChatMarkdown text={plan} mentions={null} names={noNames} className="text-[0.8125rem]" />
        ) : (
          <p className="text-[0.8125rem] text-subtle">{t('plan.empty')}</p>
        )}
      </div>
    </DecisionCard>
  )
}
