import { useState } from 'react'
import { ArchiveIcon, BookOpenIcon, ChevronRightIcon, CornerDownRightIcon, GitMergeIcon } from 'lucide-react'
import { Link } from 'react-router'
import type { Task } from '@/api/types'
import { AgentAvatar } from '@/components/shared/agent-avatar'
import { StatusMark } from '@/components/shared/status-mark'
import { UserAvatar } from '@/components/shared/user-avatar'
import { BranchDialog, type BranchDialogState } from '@/features/branches/BranchDialog'
import { wikiHref } from '@/features/wiki/links'
import type { AgentLook } from '@/lib/agentLooks'
import { formatAgo, formatElapsed } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { markOf } from './tasks'

export interface TaskCardProps {
  task: Task
  roomId: string
  // The member's name and look; the column names the member when grouped
  // by member, and the card leads with its mark instead.
  name: string
  look?: AgentLook
  byMember?: boolean
  now: number
  onOpenThread: (threadId: string) => void
}

// TaskCard is one task on the board (docs/webui.md 4.20), the way Multica
// and Vibe Kanban draw one: the topic's number and how long ago, or how
// long it has been going; what was asked, two lines at most; the work it
// is part of, or how many of its parts are done; who does it and one
// state. The card opens the piece of work; a request waiting opens its
// topic; work to merge, the merge itself (docs/design.md 5.21).
export function TaskCard({ task, roomId, name, look, byMember, now, onOpenThread }: TaskCardProps) {
  const t = useT()
  const title = task.title || t('tasks.untitled', { n: task.thread_number })
  const running = task.state === 'running'
  return (
    <article className="relative flex flex-col gap-2 rounded-[0.625rem] border bg-card px-3 pt-2.75 pb-2.5 shadow-xs transition-colors hover:border-input has-[a[data-card]:focus-visible]:ring-2 has-[a[data-card]:focus-visible]:ring-ring/50">
      <div className="flex items-center gap-2 text-[0.71875rem] text-subtle tabular-nums">
        {byMember ? <StatusMark kind={markOf(task)} className="size-3.25" /> : null}
        <span className="font-mono">#{task.thread_number}</span>
        <span className="grow" />
        {running ? (
          <span className="flex items-center gap-1.5 font-mono text-status-run">
            <span aria-hidden="true" className="size-1.25 rounded-full bg-current" />
            {formatElapsed(task.started_at, now)}
          </span>
        ) : task.ended_at ? (
          <span>{formatAgo(task.ended_at, new Date(now))}</span>
        ) : null}
      </div>
      <h3 className="line-clamp-2 text-[0.84375rem] leading-[1.45] font-medium text-foreground">
        <Link
          data-card
          to={`/rooms/${roomId}/tasks/${task.chain}`}
          className="outline-none after:absolute after:inset-0 after:rounded-[0.625rem] after:content-['']"
        >
          {title}
        </Link>
      </h3>
      {task.work ? (
        <p className="flex min-w-0 items-center gap-1.5 text-xs text-subtle">
          <CornerDownRightIcon aria-hidden="true" className="size-3 flex-none" />
          <span className="sr-only">{t('tasks.partOf')}</span>
          <span className="min-w-0 truncate">{task.work.title || t('tasks.untitled', { n: task.work.thread_number })}</span>
        </p>
      ) : null}
      {task.parts ? <Parts done={task.parts.done} total={task.parts.total} /> : null}
      <div className={cn('flex min-h-6 items-center gap-2', byMember ? 'justify-end' : 'mt-0.5')}>
        {byMember ? null : (
          <>
            {look ? <AgentAvatar look={look} name={name} size="sm" mark={false} /> : <UserAvatar name={name} size="sm" />}
            <span className="min-w-0 truncate text-[0.78125rem] text-body">{name}</span>
            <span className="grow" />
          </>
        )}
        <Corner task={task} roomId={roomId} name={name} onOpenThread={onOpenThread} />
      </div>
    </article>
  )
}

// Parts is how many of the tasks one handed on are done: one bar, one
// colour.
function Parts({ done, total }: { done: number; total: number }) {
  const t = useT()
  return (
    <div className="flex items-center gap-2.5" role="img" aria-label={t('tasks.parts', { done, total })}>
      <span className="h-1 grow overflow-hidden rounded-full bg-chart-track">
        <span className="block h-full rounded-full bg-foreground/55" style={{ width: `${total ? (done / total) * 100 : 0}%` }} />
      </span>
      <span aria-hidden="true" className="font-mono text-[0.71875rem] text-subtle tabular-nums">
        {done}/{total}
      </span>
    </div>
  )
}

// Corner is the one state a card ends with: a request waiting on a
// person, work to merge, the commit it was merged in, its work archived,
// or the wiki page it wrote.
function Corner({ task, roomId, name, onOpenThread }: { task: Task; roomId: string; name: string; onOpenThread: (threadId: string) => void }) {
  const t = useT()
  const [dialog, setDialog] = useState<BranchDialogState>()
  const chip = 'relative z-10 flex h-6 flex-none items-center gap-1.25 rounded-[0.4375rem] text-xs'
  if (task.waiting) {
    return (
      <button
        type="button"
        onClick={() => onOpenThread(task.thread_id)}
        className={cn(chip, 'border border-status-wait/30 bg-status-wait/10 pr-1.5 pl-1.5 font-medium text-status-wait')}
      >
        <StatusMark kind="wait" className="size-3" />
        {t('tasks.waiting')}
        <ChevronRightIcon aria-hidden="true" className="size-3" />
      </button>
    )
  }
  if (task.state === 'merge') {
    return (
      <>
        <button
          type="button"
          onClick={() => setDialog({ kind: 'merge', memberId: task.member_id, name })}
          className={cn(chip, 'border px-2 text-foreground hover:bg-muted')}
        >
          <GitMergeIcon aria-hidden="true" className="size-3.25 text-status-merge" />
          {t('tasks.merge')}
        </button>
        <BranchDialog roomId={roomId} state={dialog} onChange={setDialog} />
      </>
    )
  }
  const outcome = task.outcome
  if (outcome?.kind === 'merged' && outcome.commit) {
    return (
      <span className={cn(chip, 'text-subtle')}>
        <GitMergeIcon aria-hidden="true" className="size-3.25 text-status-ok" />
        <span className="sr-only">{t('tasks.mergedIn')}</span>
        <code className="font-mono text-[0.71875rem]" translate="no">
          {outcome.commit.slice(0, 7)}
        </code>
      </span>
    )
  }
  if (outcome?.kind === 'archived') {
    return (
      <span className={cn(chip, 'text-subtle')}>
        <ArchiveIcon aria-hidden="true" className="size-3.25" />
        {t('tasks.archived')}
      </span>
    )
  }
  if (outcome?.kind === 'wiki' && outcome.page) {
    return (
      <Link to={wikiHref(roomId, outcome.page)} className={cn(chip, 'min-w-0 text-subtle hover:text-foreground')}>
        <BookOpenIcon aria-hidden="true" className="size-3.25 flex-none" />
        <code className="min-w-0 truncate font-mono text-[0.71875rem]" translate="no">
          {pageName(outcome.page)}
        </code>
      </Link>
    )
  }
  return null
}

// pageName is a wiki page as a card names it: its file, without the .md.
export function pageName(path: string): string {
  return (path.split('/').pop() ?? path).replace(/\.md$/, '')
}
