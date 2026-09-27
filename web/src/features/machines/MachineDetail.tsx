import { ChevronLeftIcon, ServerIcon } from 'lucide-react'
import { lazy, Suspense, type ReactNode } from 'react'
import { Link } from 'react-router'
import { useAgents, useMachineActivity, useMachineMembers } from '@/api/agents'
import type { Machine } from '@/api/types'
import { RuntimeIcon } from '@/components/shared/runtime-icon'
import { StatusPill } from '@/components/shared/status-pill'
import { Button } from '@/components/ui/button'
import { Empty, EmptyContent, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Item, ItemActions, ItemContent, ItemGroup, ItemMedia, ItemTitle } from '@/components/ui/item'
import { Skeleton } from '@/components/ui/skeleton'
import { runtimeName, installCommand, installableRuntimes } from '@/lib/runtimes'
import { formatAgoParts } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { CommandButton } from './CommandButton'
import { accountPause, usePauses } from '@/api/pauses'
import { RuntimeItem } from './RuntimeItem'
import { MachineAgents } from './MachineAgents'
import { RecheckButton } from './RecheckButton'
import { Rows } from './Rows'
import { detectedRuntimes } from './machines'

// The charts come with Recharts, which only this page needs: it loads when
// a machine is first opened, not with the app.
const ActivitySection = lazy(() => import('./ActivitySection').then((module) => ({ default: module.ActivitySection })))

export interface MachineDetailProps {
  machine: Machine
  now: number
  className?: string
}

// One machine, beside the list on a wide screen and on its own on a phone:
// the machine and when it was heard from, a line of counts, what the
// machine did over the last day, the runtimes it detected, then every agent,
// running or idle here. One column, each section the full width.
// What is not installed there is not listed; a machine with nothing found
// says how to install one.
export function MachineDetail({ machine, now, className }: MachineDetailProps) {
  const t = useT()
  const agents = useAgents()
  const mine = agents.data?.filter((agent) => agent.machine_id === machine.id)
  const members = useMachineMembers(machine.id)
  const activity = useMachineActivity(machine.id)
  const pauses = usePauses()
  const runtimes = detectedRuntimes(machine)
  const at = new Date(now)

  return (
    <div className={cn('min-w-0 flex-1 flex-col overflow-y-auto', className)}>
      <div className="@container mx-auto flex w-full max-w-280 flex-col gap-6 px-5 py-5 md:px-7 md:py-6">
        <Link to="/machines" className="-mb-2 flex items-center gap-1 self-start text-xs text-subtle hover:text-foreground md:hidden">
          <ChevronLeftIcon className="size-3.5" />
          {t('machines.backToList')}
        </Link>
        <header className="flex items-start gap-4">
          <div className="min-w-0 flex-1">
            <div className="flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-1">
              <h2 className="min-w-0 truncate text-lg font-semibold tracking-[-0.01em]" translate="no">
                {machine.name}
              </h2>
              <StatusPill tone="ok">{t('machines.online')}</StatusPill>
            </div>
            <p className="mt-1 text-xs text-subtle">
              <TimeFact phrase={(ago) => t('machines.connectedAgo', { ago })} iso={machine.connected_at} now={at} />
              {' · '}
              <TimeFact phrase={(ago) => t('machines.heartbeatAgo', { ago })} iso={machine.last_seen} now={at} />
              {machine.probed_at ? (
                <>
                  {' · '}
                  <TimeFact phrase={(ago) => t('machines.probedAgo', { ago })} iso={machine.probed_at} now={at} />
                </>
              ) : null}
            </p>
          </div>
          <RecheckButton machines={[machine]} />
        </header>

        <p className="flex flex-wrap gap-x-6 gap-y-1 text-[0.8125rem] text-muted-foreground">
          {machine.runtimes !== null ? <Count n={runtimes.length} label={t('machines.kpiRuntimes', { n: runtimes.length })} /> : null}
          {mine ? <Count n={mine.length} label={t('machines.kpiAgents', { n: mine.length })} /> : null}
          {members.data ? <Count n={new Set(members.data.filter((m) => m.turn).map((m) => m.member.agent_id)).size} label={t('machines.kpiRunning')} /> : null}
          {activity.data ? <Count n={activity.data.turns} label={t('machines.kpiTurns', { n: activity.data.turns })} /> : null}
        </p>

        <Section title={t('machines.activity')}>
          <Suspense fallback={<ActivityPlaceholder />}>
            <ActivitySection machineId={machine.id} runtimes={runtimes.map((runtime) => runtime.info.name)} />
          </Suspense>
        </Section>

        <Section title={t('machines.runtimes')}>
          {machine.runtimes === null ? (
            <p className="text-[0.8125rem] text-subtle">{t('machines.probing')}</p>
          ) : runtimes.length === 0 ? (
            <NothingDetected />
          ) : (
            <Rows>
              {runtimes.map((runtime) => (
                <RuntimeItem
                  key={runtime.info.name}
                  runtime={runtime}
                  quota={machine.quotas?.[runtime.info.name]}
                  pause={accountPause(pauses.data, machine.id, runtime.info.name)}
                />
              ))}
            </Rows>
          )}
        </Section>

        <Section title={t('machines.agents')}>
          <MachineAgents machineId={machine.id} agents={agents} members={members} />
        </Section>
      </div>
    </div>
  )
}

// No machine picked yet: the pane beside the list asks for one. Only on a
// wide screen; a phone shows the list alone.
export function MachinePick({ className }: { className?: string }) {
  const t = useT()
  return (
    <div className={cn('min-w-0 flex-1 flex-col', className)}>
      <Empty>
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <ServerIcon />
          </EmptyMedia>
          <EmptyTitle>{t('machines.pick')}</EmptyTitle>
        </EmptyHeader>
      </Empty>
    </div>
  )
}

// A machine in the address that is not connected. On a phone this pane is
// all there is, so it offers the way back.
export function MachineGone({ className }: { className?: string }) {
  const t = useT()
  return (
    <div className={cn('min-w-0 flex-1 flex-col', className)}>
      <Empty>
        <EmptyHeader>
          <EmptyTitle>{t('machines.gone')}</EmptyTitle>
        </EmptyHeader>
        <EmptyContent>
          <Button variant="outline" size="sm" asChild>
            <Link to="/machines">{t('machines.backToList')}</Link>
          </Button>
        </EmptyContent>
      </Empty>
    </div>
  )
}

const SLOT = '\u0000'

// One of the header's facts with its time picked out, the way the counts
// below pick out their numbers: "Connected 1h ago", with "1h" in the
// foreground. phrase is the sentence with the time put in.
function TimeFact({ phrase, iso, now }: { phrase: (ago: string) => string; iso: string; now: Date }) {
  const [before, after = ''] = phrase(SLOT).split(SLOT)
  const [amount, rest] = formatAgoParts(iso, now)
  return (
    <span>
      {before}
      <span className="font-medium text-foreground">{amount}</span>
      {rest}
      {after}
    </span>
  )
}

function Count({ n, label }: { n: number; label: string }) {
  return (
    <span>
      <b className="mr-1 font-semibold text-foreground tabular-nums">{n}</b>
      {label}
    </span>
  )
}

// Where the activity cards will be while their code loads.
function ActivityPlaceholder() {
  const t = useT()
  return (
    <div role="status" aria-label={t('common.loading')} className="flex flex-col gap-4">
      <Skeleton className="h-80 w-full rounded-xl" />
      <Skeleton className="h-80 w-full rounded-xl" />
    </div>
  )
}

// A titled block of the detail; the title names the region for a screen
// reader too.
function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section aria-label={title} className="flex min-w-0 flex-col gap-2">
      <h3 className="text-xs font-medium text-subtle">{title}</h3>
      {children}
    </section>
  )
}

// Nothing installed that Veyloom can drive: how to get one.
function NothingDetected() {
  const t = useT()
  return (
    <Empty className="gap-3 p-4 md:p-5">
      <EmptyHeader className="max-w-none">
        <EmptyTitle className="text-[0.8125rem] font-normal tracking-normal text-muted-foreground">{t('machines.noneDetected')}</EmptyTitle>
      </EmptyHeader>
      <EmptyContent className="max-w-md">
        <ItemGroup className="w-full gap-1">
          {installableRuntimes().map((name) => (
            <Item key={name} role="listitem" size="sm" className="flex-wrap gap-x-3 gap-y-1.5 px-0 py-1">
              <ItemMedia>
                <RuntimeIcon runtime={name} className="size-5" />
              </ItemMedia>
              <ItemContent>
                <ItemTitle className="text-[0.8125rem] font-normal" translate="no">
                  {runtimeName(name)}
                </ItemTitle>
              </ItemContent>
              <ItemActions>
                <CommandButton command={installCommand(name) ?? ''} />
              </ItemActions>
            </Item>
          ))}
        </ItemGroup>
      </EmptyContent>
    </Empty>
  )
}
