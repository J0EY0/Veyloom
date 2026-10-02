import type { Agent } from '@/api/types'
import { Field, FieldContent, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Switch } from '@/components/ui/switch'
import { useT } from '@/lib/i18n'
import { byLeader, KeeperSelect } from './MaintainerFields'
import { noWikiReadOnly, useRuntimeTraits, type RuntimeTraits } from '@/api/runtimes'

export interface NewProjectMaintainerProps {
  id: string
  // The agents picked for the project, in the order they join: the first
  // leads it (docs/design.md 5.21).
  agents: Agent[]
  upkeep: boolean
  // The agent chosen to keep the wiki, or byLeader (keeperFor); none when
  // none of them can.
  value?: string
  onUpkeep: (on: boolean) => void
  onChange: (agentId: string) => void
}

// keeperFor is who keeps the wiki of a new project: the one asked for if
// it can write the wiki, else the leader, the first agent, if it can, else
// the first agent who can; none when none of them can.
export function keeperFor(agents: Agent[], asked: string, traits: Record<string, RuntimeTraits> | undefined): string | undefined {
  const agentOf = (value: string) => (value === byLeader ? agents[0] : agents.find((agent) => agent.id === value))
  const can = (value: string) => {
    const agent = agentOf(value)
    return agent !== undefined && !noWikiReadOnly(traits, agent.runtime, agent.permission_preset)
  }
  return [asked, byLeader, ...agents.map((agent) => agent.id)].find(can)
}

// The wiki maintainer a project may start with (docs/design.md 5.16,
// 5.21): off, the default, in which case the chat offers it after a few
// topics; or on, daily, kept by the leader unless one of the agents picked
// is chosen instead. One who cannot write the wiki is not offered; with
// nobody who can, it cannot be turned on.
export function NewProjectMaintainer({ id, agents, upkeep, value, onUpkeep, onChange }: NewProjectMaintainerProps) {
  const t = useT()
  const traits = useRuntimeTraits()
  const candidates = agents.map((agent) => ({ id: agent.id, display_name: agent.name }))
  const canKeep = (option: string) => keeperFor(agents, option, traits.data) === option
  return (
    <>
      <Field orientation="horizontal">
        <FieldContent>
          <FieldLabel htmlFor={`${id}-upkeep`}>{t('project.maintainer')}</FieldLabel>
          <FieldDescription>{t('project.maintainerHint')}</FieldDescription>
        </FieldContent>
        <Switch id={`${id}-upkeep`} checked={upkeep && value !== undefined} disabled={value === undefined} onCheckedChange={onUpkeep} />
      </Field>
      {upkeep && value !== undefined ? (
        <Field>
          <FieldLabel htmlFor={`${id}-maintainer`}>{t('maintainer.who')}</FieldLabel>
          <KeeperSelect
            id={`${id}-maintainer`}
            value={value}
            leader={agents[0] ? { display_name: agents[0].name } : undefined}
            candidates={candidates}
            canKeep={canKeep}
            onChange={onChange}
          />
        </Field>
      ) : null}
    </>
  )
}
