import type { ReactNode } from 'react'
import { ArrowLeftIcon, GitMergeIcon, MessageCircleIcon, RotateCcwIcon } from 'lucide-react'
import { Link } from 'react-router'
import { useRoomMembers } from '@/api/agents'
import { errorText } from '@/api/errorText'
import { useProject } from '@/api/projects'
import { useRoom } from '@/api/rooms'
import type { Work, WorkEvent } from '@/api/types'
import { useWork } from '@/api/work'
import { AgentAvatar } from '@/components/shared/agent-avatar'
import { StatusMark } from '@/components/shared/status-mark'
import { UserAvatar } from '@/components/shared/user-avatar'
import { Button } from '@/components/ui/button'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { useMentionTargets } from '@/features/rooms/useMentionTargets'
import { formatCompactCount, formatDay, formatHour, formatSpan } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { totalTokens } from '@/lib/tokens'
import { useNow } from '@/lib/useNow'
import { Waterfall } from './Waterfall'
import { time } from './work'

export interface WorkViewProps {
  roomId: string
  chain: string
  onOpenThread: (threadId: string) => void
}

// WorkView is a piece of work's page (docs/webui.md 4.20): what was asked,
// a row of figures, and its turns as a waterfall, with what became of the
// branches it changed below.
export function WorkView({ roomId, chain, onOpenThread }: WorkViewProps) {
  const t = useT()
  const work = useWork(chain)
  const { names, looks } = useMentionTargets(roomId)
  const now = useNow(work.data?.running ?? false)
  if (work.isPending) {
    return (
      <div role="status" aria-label={t('common.loading')} className="flex flex-col gap-4 px-6 pt-4">
        <Skeleton className="h-4 w-24" />
        <Skeleton className="h-7 w-2/3" />
        <Skeleton className="h-48 w-full rounded-xl" />
      </div>
    )
  }
  if (work.isError) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>{t('work.failed')}</EmptyTitle>
          <EmptyDescription>{errorText(work.error)}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }
  const w = work.data
  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <div className="mx-auto flex w-full max-w-[76rem] flex-col gap-4 px-6 pt-3.5 pb-8">
        <div className="flex items-center gap-2">
          <nav aria-label={t('work.where')} className="flex items-center gap-1.5 text-[0.78125rem] text-subtle">
            <Link to={`/rooms/${roomId}/tasks`} className="flex items-center gap-1.25 text-muted-foreground hover:text-foreground">
              <ArrowLeftIcon aria-hidden="true" className="size-3.5" />
              {t('room.tab.tasks')}
            </Link>
            <span aria-hidden="true">/</span>
            <span className="font-mono">#{w.thread_number}</span>
          </nav>
          <span className="grow" />
          <Button variant="outline" size="sm" onClick={() => onOpenThread(w.thread_id)} data-opens-panel>
            <MessageCircleIcon aria-hidden="true" className="text-subtle" />
            {t('work.openTopic', { n: w.thread_number })}
          </Button>
        </div>
        <header className="flex flex-col gap-2.5">
          <h1 className="text-2xl font-semibold tracking-[-0.02em]">{w.title || t('tasks.untitled', { n: w.thread_number })}</h1>
          <Byline work={w} roomId={roomId} names={names} looks={looks} />
        </header>
        {w.ask && w.ask !== w.title ? <p className="max-w-[54rem] text-sm leading-[1.75] text-body">{w.ask}</p> : null}
        <Figures work={w} now={now} />
        <section aria-label={t('work.process')} className="flex flex-col gap-1.5 rounded-xl border px-4.5 pt-3.5 pb-3">
          <div className="overflow-x-auto">
            <div className="min-w-[48rem]">
              <Waterfall work={w} names={names} looks={looks} now={now} />
            </div>
          </div>
          {w.events.length > 0 ? (
            <ul className="mt-2 flex flex-col border-t pt-2">
              {w.events.map((event) => (
                <Event key={event.kind + event.commit + event.ref} event={event} roomId={roomId} names={names} />
              ))}
            </ul>
          ) : null}
        </section>
      </div>
    </div>
  )
}

interface BylineProps {
  work: Work
  roomId: string
  names: ReadonlyMap<string, string>
  looks: ReadonlyMap<string, import('@/lib/agentLooks').AgentLook>
}

// Byline is how the work stands, who asked when, and whom.
function Byline({ work, roomId, names, looks }: BylineProps) {
  const t = useT()
  const room = useRoom(roomId)
  const project = useProject(room.data?.project_id ?? '')
  const asker = work.asked_by ? names.get(work.asked_by) : undefined
  return (
    <div className="flex flex-wrap items-center gap-2 text-[0.8125rem] text-muted-foreground">
      <span className="inline-flex h-6 items-center gap-1.5 rounded-full border pr-2.5 pl-2 text-foreground">
        <StatusMark kind={work.running ? 'run' : 'done'} className="size-3.25" />
        {work.running ? t('tasks.state.running') : t('work.finished')}
      </span>
      {asker ? (
        <span className="ml-1 flex items-center gap-1.5">
          <UserAvatar name={asker} size="xs" />
          {t('work.askedBy', { name: asker })}
        </span>
      ) : null}
      <Dot />
      <span>
        {formatDay(work.started_at)} {formatHour(work.started_at)}
      </span>
      {work.asked.map((id) => {
        const name = names.get(id) ?? ''
        const look = looks.get(id)
        return (
          <span key={id} className="flex items-center gap-1.5">
            <Dot />
            {project?.leader_id === id ? t('work.leader') : null}
            {look ? <AgentAvatar look={look} name={name} size="xs" mark={false} /> : null}
            {name}
          </span>
        )
      })}
    </div>
  )
}

function Dot() {
  return (
    <span aria-hidden="true" className="text-border">
      ·
    </span>
  )
}

// Figures is the work in a row of plain numbers: how long, how many
// tokens and turns, how long it waited for a person, the commit it went
// on the main line in.
function Figures({ work, now }: { work: Work; now: number }) {
  const t = useT()
  const end = work.ended_at ? time(work.ended_at) : now
  const merged = work.events.find((event) => event.kind === 'merged' && event.commit)
  const items: { label: ReactNode; value: ReactNode; mono?: boolean }[] = [
    { label: t('work.took'), value: formatSpan(end - time(work.started_at)) },
    { label: t('work.tokens'), value: formatCompactCount(totalTokens(work.usage)) },
    { label: t('work.turnCount'), value: String(work.turns.length) },
    {
      label: (
        <>
          <span aria-hidden="true" className="size-1.5 rounded-full bg-status-wait" />
          {t('work.waitedForYou')}
        </>
      ),
      value: work.waited_ms > 0 ? formatSpan(work.waited_ms) : '—',
    },
  ]
  if (merged?.commit) {
    items.push({
      label: (
        <>
          <GitMergeIcon aria-hidden="true" className="size-3.25 text-status-ok" />
          {t('work.merged')}
        </>
      ),
      value: merged.commit.slice(0, 7),
      mono: true,
    })
  }
  return (
    <dl className="flex flex-wrap items-stretch border-t pt-4 pb-0.5">
      {items.map((item, index) => (
        <div key={index} className="flex flex-col gap-1.5 border-l px-7 first:border-l-0 first:pl-0">
          <dt className="flex items-center gap-1.5 text-xs text-subtle">{item.label}</dt>
          <dd className={item.mono ? 'font-mono text-[1.0625rem] font-semibold' : 'text-[1.1875rem] font-semibold tracking-[-0.01em] tabular-nums'}>
            {item.value}
          </dd>
        </div>
      ))}
    </dl>
  )
}

// Event is what became of branches the work changed: merged, with whose
// changes, or reset with the ref its work is archived under.
function Event({ event, roomId, names }: { event: WorkEvent; roomId: string; names: ReadonlyMap<string, string> }) {
  const t = useT()
  const members = useRoomMembers(roomId, { removed: true })
  const who = event.members
    .map((id) => names.get(id) ?? '')
    .filter(Boolean)
    .join(t('common.listSeparator'))
  const branches = event.members.map((id) => members.data?.find((m) => m.id === id)?.branch ?? names.get(id) ?? '').filter(Boolean)
  return (
    <li className="flex min-h-7.5 items-center gap-2.5 text-[0.8125rem]">
      {event.kind === 'merged' ? (
        <GitMergeIcon aria-hidden="true" className="size-3.5 flex-none text-status-ok" />
      ) : (
        <RotateCcwIcon aria-hidden="true" className="size-3.5 flex-none text-subtle" />
      )}
      <span className="flex min-w-0 flex-1 flex-wrap items-center gap-x-1.5">
        {event.kind === 'merged' ? (
          <>
            {t('work.mergedInto')}
            <code className="font-mono text-xs" translate="no">
              {event.commit?.slice(0, 7)}
            </code>
            <span className="text-subtle">{t('work.carrying', { names: who })}</span>
          </>
        ) : (
          <>
            <code className="font-mono text-xs" translate="no">
              {branches.join(', ')}
            </code>
            {t('work.resetTo')}
            <span className="text-subtle">{t('work.archivedAs')}</span>
            <code className="min-w-0 truncate font-mono text-xs text-subtle" translate="no">
              {event.ref}
            </code>
          </>
        )}
      </span>
      <span className="flex-none font-mono text-[0.71875rem] text-subtle">{formatHour(event.at)}</span>
    </li>
  )
}
