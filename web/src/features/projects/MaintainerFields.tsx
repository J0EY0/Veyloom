import { useAgents, useRoomMembers } from '@/api/agents'
import type { Member, Project, UpkeepTrigger } from '@/api/types'
import { keepsNoWiki, useUpkeepStatus } from '@/api/upkeep'
import { useRuntimeTraits } from '@/api/runtimes'
import { runtimeName } from '@/lib/runtimes'
import { Field, FieldContent, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { TriggerSelect } from '@/features/wiki/TriggerSelect'
import { useT } from '@/lib/i18n'

// Radix gives no item an empty value; this one stands for the project's
// leader keeping the wiki, as nobody else was chosen (docs/design.md 5.21).
export const byLeader = 'leader'

// upkeepOff stands for nobody keeping the wiki, where the same list turns
// its upkeep off.
export const upkeepOff = 'off'

export interface MaintainerFieldsProps {
  project: Project
  id: string
  upkeep: boolean
  // A member's id, or byLeader.
  member: string
  trigger: UpkeepTrigger
  onUpkeep: (on: boolean) => void
  onMember: (member: string) => void
  onTrigger: (trigger: UpkeepTrigger) => void
}

// The project's wiki maintainer, in its settings (docs/design.md 5.12,
// 5.21): whether the wiki is kept at all, by whom, the leader unless a
// person picks someone else, and when it goes over the chat.
export function MaintainerFields({ project, id, upkeep, member, trigger, onUpkeep, onMember, onTrigger }: MaintainerFieldsProps) {
  const t = useT()
  const members = useRoomMembers(project.main_room_id)
  const agents = useAgents()
  const traits = useRuntimeTraits()
  const status = useUpkeepStatus(project.id)
  const minutes = status.data?.idle_minutes ?? 30
  const candidates = (members.data ?? []).filter((m) => m.enabled || m.id === member)
  const leader = (members.data ?? []).find((m) => m.id === project.leader_id)
  const keeper = member === byLeader ? leader : candidates.find((m) => m.id === member)
  const noWiki = keepsNoWiki(keeper, agents.data, traits.data)
  return (
    <>
      <Field orientation="horizontal">
        <FieldContent>
          <FieldLabel htmlFor={`${id}-upkeep`}>{t('maintainer.title')}</FieldLabel>
          <FieldDescription>{t('maintainer.memberHint')}</FieldDescription>
        </FieldContent>
        <Switch id={`${id}-upkeep`} checked={upkeep} onCheckedChange={onUpkeep} />
      </Field>
      {upkeep ? (
        <>
          <Field>
            <FieldLabel htmlFor={`${id}-maintainer`}>{t('maintainer.who')}</FieldLabel>
            <KeeperSelect id={`${id}-maintainer`} value={member} leader={leader} candidates={candidates} onChange={onMember} />
            {noWiki ? (
              <FieldDescription className="text-status-wait">{t('maintainer.readOnlyNoWiki', { runtime: runtimeName(noWiki) })}</FieldDescription>
            ) : null}
          </Field>
          <Field>
            <FieldLabel htmlFor={`${id}-trigger`}>{t('maintainer.trigger')}</FieldLabel>
            <TriggerSelect id={`${id}-trigger`} className="w-full" value={trigger} idleMinutes={minutes} onChange={onTrigger} />
          </Field>
        </>
      ) : null}
    </>
  )
}

export interface KeeperSelectProps {
  id?: string
  // A member's id, or byLeader.
  value: string
  // The project's leader as it stands, if it has one.
  leader?: Pick<Member, 'display_name'>
  candidates: Pick<Member, 'id' | 'display_name'>[]
  onChange: (value: string) => void
  size?: 'sm' | 'default'
  className?: string
  disabled?: boolean
  label?: string
  // Offers upkeepOff first, under this name.
  offLabel?: string
}

// KeeperSelect picks who keeps the wiki: the leader, first and the
// default, or one of the members by name; where offLabel is given, nobody.
export function KeeperSelect({ id, value, leader, candidates, onChange, size, className = 'w-full', disabled, label, offLabel }: KeeperSelectProps) {
  const t = useT()
  return (
    <Select value={value} onValueChange={onChange} disabled={disabled}>
      <SelectTrigger id={id} size={size} className={className} aria-label={label}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {offLabel ? <SelectItem value={upkeepOff}>{offLabel}</SelectItem> : null}
        <SelectItem value={byLeader}>{leader ? t('maintainer.byLeader', { name: leader.display_name }) : t('maintainer.byLeaderNone')}</SelectItem>
        {candidates.map((m) => (
          <SelectItem key={m.id} value={m.id}>
            {m.display_name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
