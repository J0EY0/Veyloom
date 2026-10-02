import type { ReactNode } from 'react'
import { toast } from 'sonner'
import { errorText } from '@/api/errorText'
import { useAgents } from '@/api/agents'
import type { Agent, WikiPageInfo } from '@/api/types'
import { useInstallSkill } from '@/api/wiki'
import { DropdownMenu, DropdownMenuCheckboxItem, DropdownMenuContent, DropdownMenuLabel, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { useT } from '@/lib/i18n'
import { runtimeName } from '@/lib/runtimes'
import { forRuntime } from '../skills'

export interface InstallMenuProps {
  name: string
  // What decides who may have it: the runtimes its tags keep it for, and
  // whether it is retired.
  skill: Pick<WikiPageInfo, 'tags' | 'status'>
  align?: 'start' | 'end'
  // The button that opens the menu.
  children: ReactNode
}

// InstallMenu installs a skill for agents or takes it off (docs/design.md
// 5.15), one tick each, staying open to do several in a row. An agent gets
// it only if its runtime is one the skill is for; a retired skill is only
// taken off. One that cannot have it is greyed, its runtime under its
// name as for every agent. Nothing to open while there is no agent.
export function InstallMenu({ name, skill, align = 'start', children }: InstallMenuProps) {
  const t = useT()
  const agents = useAgents()
  const install = useInstallSkill()
  if (!agents.data || agents.data.length === 0) return null

  function barred(agent: Agent): boolean {
    if (agent.skills.includes(name)) return false
    return skill.status === 'deprecated' || !forRuntime(skill, agent.runtime)
  }

  function toggle(agent: Agent, on: boolean) {
    install.mutate(
      { name, agentId: agent.id, installed: on },
      { onError: (err) => toast.error(t(on ? 'skill.installFailed' : 'skill.uninstallFailed', { error: errorText(err) })) },
    )
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
      <DropdownMenuContent align={align} className="w-64">
        <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">{t('skill.installFor')}</DropdownMenuLabel>
        {agents.data.map((agent) => (
          <DropdownMenuCheckboxItem
            key={agent.id}
            checked={agent.skills.includes(name)}
            disabled={barred(agent) || install.isPending}
            // Stays open to install for several in a row.
            onSelect={(event) => event.preventDefault()}
            onCheckedChange={(on) => toggle(agent, on === true)}
          >
            <span className="flex min-w-0 flex-col">
              <span className="truncate">{agent.name}</span>
              <span className="truncate text-xs text-muted-foreground">{runtimeName(agent.runtime)}</span>
            </span>
          </DropdownMenuCheckboxItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
