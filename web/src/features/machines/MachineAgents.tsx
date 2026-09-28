import { useMemo } from 'react'
import type { UseQueryResult } from '@tanstack/react-query'
import { Link } from 'react-router'
import type { Agent, MachineMember } from '@/api/types'
import { AgentAvatar } from '@/components/shared/agent-avatar'
import { StatusDot } from '@/components/shared/status-dot'
import { Empty, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Item, ItemActions, ItemContent, ItemDescription, ItemMedia, ItemTitle } from '@/components/ui/item'
import { runtimeName } from '@/lib/runtimes'
import { useT } from '@/lib/i18n'
import { Rows, RowsSkeleton } from './Rows'
import { machineAgents, type MachineAgent } from './machines'
import { errorText } from '@/api/errorText'

export interface MachineAgentsProps {
  machineId: string
  // Every agent; the ones set up on this machine are listed.
  agents: UseQueryResult<Agent[]>
  members: UseQueryResult<MachineMember[]>
}

// The agents set up on this machine, listed as soon as they are made,
// running or idle. A row with a member leads to where it works.
export function MachineAgents({ machineId, agents, members }: MachineAgentsProps) {
  const t = useT()
  const rows = useMemo(
    () =>
      agents.data && members.data
        ? machineAgents(
            agents.data.filter((agent) => agent.machine_id === machineId),
            members.data,
          )
        : [],
    [agents.data, members.data, machineId],
  )

  if (agents.isPending || members.isPending) return <RowsSkeleton label={t('common.loading')} />
  const error = agents.error ?? members.error
  if (error) return <p className="text-[0.8125rem] text-subtle">{t('machines.agentsFailed', { error: errorText(error) })}</p>
  if (rows.length === 0) {
    return (
      <Empty className="p-4 md:p-5">
        <EmptyHeader>
          <EmptyTitle className="text-[0.8125rem] font-normal tracking-normal text-subtle">{t('machines.agentsEmpty')}</EmptyTitle>
        </EmptyHeader>
      </Empty>
    )
  }
  return (
    <Rows>
      {rows.map((row) => (
        <AgentRow key={row.agent.id} row={row} />
      ))}
    </Rows>
  )
}

function AgentRow({ row }: { row: MachineAgent }) {
  const t = useT()
  const { agent, state, members } = row
  const lead = members[0]
  const projects = [...new Set(members.map((member) => member.project_name))]
  const to = lead?.turn ? `/rooms/${lead.member.room_id}?thread=${lead.turn.thread_id}` : lead ? `/rooms/${lead.member.room_id}?panel=members` : undefined
  const className = 'flex-nowrap gap-3 rounded-none py-3'
  const content = (
    <>
      <ItemMedia>
        <AgentAvatar look={agent} name={agent.name} size="message" />
      </ItemMedia>
      <ItemContent className="min-w-0 gap-0.5">
        <ItemTitle className="max-w-full truncate">{agent.name}</ItemTitle>
        <ItemDescription className="truncate text-xs text-subtle">
          {[runtimeName(agent.runtime), projects.join(t('common.listSeparator'))].filter(Boolean).join(' · ')}
        </ItemDescription>
      </ItemContent>
      <ItemActions className="flex-none gap-2 text-xs whitespace-nowrap text-muted-foreground">
        <StatusDot tone={state === 'running' ? 'run' : 'idle'} />
        {state === 'running' ? t('machines.agentRunning') : t('machines.agentIdle')}
      </ItemActions>
    </>
  )
  return (
    <div role="listitem">
      {to ? (
        <Item asChild size="sm" className={className}>
          <Link to={to}>{content}</Link>
        </Item>
      ) : (
        <Item size="sm" className={className}>
          {content}
        </Item>
      )}
    </div>
  )
}
