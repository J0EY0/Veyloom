import type { Agent } from '@/api/types'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useT } from '@/lib/i18n'
import { noMaintainer } from './MaintainerFields'

export interface NewProjectMaintainerProps {
  id: string
  // The agents picked for the project, in the order they join.
  agents: Agent[]
  // The agent chosen, or noMaintainer.
  value: string
  onChange: (agentId: string) => void
}

// The wiki maintainer a project may start with (docs/design.md 5.16): one of
// the agents picked, keeping the wiki daily, or none yet, the default, in
// which case the chat offers one after a few topics.
export function NewProjectMaintainer({ id, agents, value, onChange }: NewProjectMaintainerProps) {
  const t = useT()
  const chosen = agents.find((agent) => agent.id === value)
  return (
    <Field>
      <FieldLabel htmlFor={`${id}-maintainer`}>{t('project.maintainer')}</FieldLabel>
      <Select value={value} onValueChange={onChange}>
        <SelectTrigger id={`${id}-maintainer`} className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={noMaintainer}>{t('project.maintainerNone')}</SelectItem>
          {agents.map((agent) => (
            <SelectItem key={agent.id} value={agent.id}>
              {agent.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <FieldDescription>{t('project.maintainerHint')}</FieldDescription>
      {chosen?.runtime === 'codex' && chosen.permission_preset === 'read_only' ? (
        <FieldDescription className="text-status-wait">{t('maintainer.readOnlyCodex')}</FieldDescription>
      ) : null}
    </Field>
  )
}
