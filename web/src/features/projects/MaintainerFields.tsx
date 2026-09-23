import { useAgents, useRoomMembers } from '@/api/agents'
import type { Project, UpkeepTrigger } from '@/api/types'
import { readOnlyCodex, useUpkeepStatus } from '@/api/upkeep'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { TriggerSelect } from '@/features/wiki/TriggerSelect'
import { useT } from '@/lib/i18n'

// Radix gives no item an empty value; this one stands for no maintainer.
export const noMaintainer = 'none'

export interface MaintainerFieldsProps {
  project: Project
  id: string
  member: string
  trigger: UpkeepTrigger
  onMember: (member: string) => void
  onTrigger: (trigger: UpkeepTrigger) => void
}

// The project's wiki maintainer, in its settings (docs/design.md 5.12):
// which member keeps the wiki, or none, and when it goes over the chat.
export function MaintainerFields({ project, id, member, trigger, onMember, onTrigger }: MaintainerFieldsProps) {
  const t = useT()
  const members = useRoomMembers(project.main_room_id)
  const agents = useAgents()
  const status = useUpkeepStatus(project.id)
  const minutes = status.data?.idle_minutes ?? 30
  const candidates = (members.data ?? []).filter((m) => m.enabled || m.id === member)
  return (
    <>
      <Field>
        <FieldLabel htmlFor={`${id}-maintainer`}>{t('maintainer.title')}</FieldLabel>
        <Select value={member} onValueChange={onMember}>
          <SelectTrigger id={`${id}-maintainer`} className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={noMaintainer}>{t('maintainer.none')}</SelectItem>
            {candidates.map((m) => (
              <SelectItem key={m.id} value={m.id}>
                {m.display_name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <FieldDescription>{t('maintainer.memberHint')}</FieldDescription>
        {readOnlyCodex(
          candidates.find((m) => m.id === member),
          agents.data,
        ) ? (
          <FieldDescription className="text-status-wait">{t('maintainer.readOnlyCodex')}</FieldDescription>
        ) : null}
      </Field>
      {member !== noMaintainer ? (
        <Field>
          <FieldLabel htmlFor={`${id}-trigger`}>{t('maintainer.trigger')}</FieldLabel>
          <TriggerSelect id={`${id}-trigger`} className="w-full" value={trigger} idleMinutes={minutes} onChange={onTrigger} />
        </Field>
      ) : null}
    </>
  )
}
