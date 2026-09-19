import type { ApprovalCardProps } from './ApprovalCard'
import { DecisionCard, type DecisionWords } from './DecisionCard'
import { confirmationOf } from './plans'

const words: DecisionWords = {
  label: 'confirm.label',
  heads: {
    pending: 'confirm.asking',
    allowed: 'confirm.confirmed',
    denied: 'confirm.refused',
    expired: 'confirm.expired',
    cancelled: 'confirm.cancelled',
  },
  yes: 'confirm.yes',
  no: 'confirm.no',
  yesBy: 'confirm.confirmedBy',
  noBy: 'confirm.refusedBy',
}

// A yes-or-no question one of pi's extensions puts to the person (docs/
// design.md 4.6): its title and message, answered yes or no. Pi takes the
// answer alone, so there is no note to add.
export function ConfirmCard(props: ApprovalCardProps) {
  const { title, message } = confirmationOf(props.approval) ?? { title: '', message: '' }
  return (
    <DecisionCard {...props} words={words}>
      <div className="grid gap-0.5 text-[0.8125rem]">
        {title ? <p className="font-medium break-words">{title}</p> : null}
        {message ? <p className="break-words whitespace-pre-wrap text-muted-foreground">{message}</p> : null}
      </div>
    </DecisionCard>
  )
}
