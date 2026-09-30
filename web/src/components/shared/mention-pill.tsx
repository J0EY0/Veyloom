import { Badge } from '@/components/ui/badge'
import { useMemberLook } from '@/lib/agentLooks'
import { AgentAvatar } from './agent-avatar'
import { UserAvatar } from './user-avatar'
import { useT } from '@/lib/i18n'

// The capsule sits on the line's baseline, and its baseline is the name's
// (the one item aligned by baseline, as tall as the capsule so it still
// reads centred): the name stands on the same line as the words around it,
// in Chinese as in English. Middle alignment centred it on the Latin
// x-height, below where Chinese sits. Overflow stays visible, which a
// baseline taken from the content needs.
const pillClass = 'h-5.5 gap-1.5 overflow-visible rounded-full border-0 py-0 pr-2 pl-[0.1875rem] align-baseline text-[0.8125rem] font-medium text-foreground'

// The name, the capsule's baseline.
function PillName({ name }: { name: string }) {
  return <span className="self-baseline leading-[1.375rem]">{name}</span>
}

// An @-mention as the design draws it: a small avatar and the name, in a
// capsule. A member (by id) shows its agent's face, a person the initial.
export function MentionPill({ name, id }: { name: string; id?: string }) {
  return (
    <Badge variant="secondary" className={pillClass}>
      <PillAvatar name={name} id={id} />
      <PillName name={name} />
    </Badge>
  )
}

export interface TakeOverButtonProps {
  name: string
  // The member handed over to.
  id?: string
  onClick: () => void
}

// An agent mentioning another agent is a hand-off a person may pick up:
// the pill is a button that prefills the composer (docs/webui.md §7 step 8).
export function TakeOverButton({ name, id, onClick }: TakeOverButtonProps) {
  const t = useT()
  return (
    <Badge asChild variant="secondary" className={`${pillClass} cursor-pointer transition-colors hover:bg-selection`}>
      <button type="button" onClick={onClick}>
        <PillAvatar name={name} id={id} />
        <PillName name={name} />
        <span className="text-xs font-normal text-subtle">{t('takeover.label')}</span>
      </button>
    </Badge>
  )
}

function PillAvatar({ name, id }: { name: string; id?: string }) {
  const look = useMemberLook(id)
  return look ? <AgentAvatar look={look} name={name} size="xs" /> : <UserAvatar name={name} size="xs" className="bg-background" />
}
