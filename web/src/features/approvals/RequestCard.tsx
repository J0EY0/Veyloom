import { ApprovalCard, type ApprovalCardProps } from './ApprovalCard'
import { ConfirmCard } from './ConfirmCard'
import { FormCard } from './FormCard'
import { LinkCard } from './LinkCard'
import { PlanCard } from './PlanCard'
import { CONFIRM_TOOL, PLAN_TOOL } from './plans'
import { QuestionCard } from './QuestionCard'

// RequestCard draws whatever a runtime asked a person, by kind: questions
// to answer, a form to fill in, a link to open, or else a permission to
// grant, a plan to approve or a yes or no to give among them (docs/
// design.md 4.6).
export function RequestCard(props: ApprovalCardProps) {
  if (props.approval.kind === 'tool_use' && props.approval.tool === PLAN_TOOL) return <PlanCard {...props} />
  if (props.approval.kind === 'tool_use' && props.approval.tool === CONFIRM_TOOL) return <ConfirmCard {...props} />
  switch (props.approval.kind) {
    case 'question':
      return <QuestionCard {...props} />
    case 'form':
      return <FormCard {...props} />
    case 'link':
      return <LinkCard {...props} />
    default:
      return <ApprovalCard {...props} />
  }
}
