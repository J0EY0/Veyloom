import { useAgents, useRoomMembers } from '@/api/agents'
import type { Member, Project, UpkeepTrigger } from '@/api/types'
import { keepsNoWiki, useUpkeepStatus } from '@/api/upkeep'
import { useRuntimeTraits } from '@/api/runtimes'
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
  // A read-only member of a runtime that then writes nothing keeps no wiki.
  const canKeep = (value: string) => !keepsNoWiki(value === byLeader ? leader : candidates.find((m) => m.id === value), agents.data, traits.data)
  // Turned on, it is kept by one who can: the one it had if it can, else
  // the leader, else the first member who can. With nobody who can, it
  // cannot be turned on; on already, it can still be turned off.
  const firstKeeper = [byLeader, ...candidates.map((m) => m.id)].find(canKeep)
  function turn(on: boolean) {
    if (on && !canKeep(member) && firstKeeper) onMember(firstKeeper)
    onUpkeep(on)
  }
  return (
    <>
      <Field orientation="horizontal">
        <FieldContent>
          <FieldLabel htmlFor={`${id}-upkeep`}>{t('maintainer.title')}</FieldLabel>
          <FieldDescription>{t('maintainer.memberHint')}</FieldDescription>
        </FieldContent>
        <Switch id={`${id}-upkeep`} checked={upkeep} disabled={!upkeep && !firstKeeper} onCheckedChange={turn} />
      </Field>
      {upkeep ? (
        <>
          <Field>
            <FieldLabel htmlFor={`${id}-maintainer`}>{t('maintainer.who')}</FieldLabel>
            <KeeperSelect id={`${id}-maintainer`} value={member} leader={leader} candidates={candidates} canKeep={canKeep} onChange={onMember} />
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
  // Whether one can keep the wiki, by its value: one who cannot is there,
  // greyed, and cannot be picked.
  canKeep?: (value: string) => boolean
}

// KeeperSelect picks who keeps the wiki: the leader, first and the
// default, or one of the members by name; where offLabel is given, nobody.
export function KeeperSelect({
  id,
  value,
  leader,
  candidates,
  onChange,
  size,
  className = 'w-full',
  disabled,
  label,
  offLabel,
  canKeep = () => true,
}: KeeperSelectProps) {
  const t = useT()
  return (
    <Select value={value} onValueChange={onChange} disabled={disabled}>
      <SelectTrigger id={id} size={size} className={className} aria-label={label}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {offLabel ? <SelectItem value={upkeepOff}>{offLabel}</SelectItem> : null}
        <SelectItem value={byLeader} disabled={!canKeep(byLeader)}>
          {leader ? t('maintainer.byLeader', { name: leader.display_name }) : t('maintainer.byLeaderNone')}
        </SelectItem>
        {candidates.map((m) => (
          <SelectItem key={m.id} value={m.id} disabled={!canKeep(m.id)}>
            {m.display_name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
