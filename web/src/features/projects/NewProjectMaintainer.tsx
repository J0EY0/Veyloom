import type { Agent } from '@/api/types'
import { Field, FieldContent, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Switch } from '@/components/ui/switch'
import { useT } from '@/lib/i18n'
import { byLeader, KeeperSelect } from './MaintainerFields'

export interface NewProjectMaintainerProps {
  id: string
  // The agents picked for the project, in the order they join: the first
  // leads it (docs/design.md 5.21).
  agents: Agent[]
  upkeep: boolean
  // The agent chosen to keep the wiki, or byLeader.
  value: string
  onUpkeep: (on: boolean) => void
  onChange: (agentId: string) => void
}

// The wiki maintainer a project may start with (docs/design.md 5.16,
// 5.21): off, the default, in which case the chat offers it after a few
// topics; or on, daily, kept by the leader unless one of the agents picked
// is chosen instead.
export function NewProjectMaintainer({ id, agents, upkeep, value, onUpkeep, onChange }: NewProjectMaintainerProps) {
  const t = useT()
  const chosen = agents.find((agent) => agent.id === value) ?? (value === byLeader ? agents[0] : undefined)
  const candidates = agents.map((agent) => ({ id: agent.id, display_name: agent.name }))
  return (
    <>
      <Field orientation="horizontal">
        <FieldContent>
          <FieldLabel htmlFor={`${id}-upkeep`}>{t('project.maintainer')}</FieldLabel>
          <FieldDescription>{t('project.maintainerHint')}</FieldDescription>
        </FieldContent>
        <Switch id={`${id}-upkeep`} checked={upkeep} onCheckedChange={onUpkeep} />
      </Field>
      {upkeep ? (
        <Field>
          <FieldLabel htmlFor={`${id}-maintainer`}>{t('maintainer.who')}</FieldLabel>
          <KeeperSelect
            id={`${id}-maintainer`}
            value={value}
            leader={agents[0] ? { display_name: agents[0].name } : undefined}
            candidates={candidates}
            onChange={onChange}
          />
          {chosen?.runtime === 'codex' && chosen.permission_preset === 'read_only' ? (
            <FieldDescription className="text-status-wait">{t('maintainer.readOnlyCodex')}</FieldDescription>
          ) : null}
        </Field>
      ) : null}
    </>
  )
}
