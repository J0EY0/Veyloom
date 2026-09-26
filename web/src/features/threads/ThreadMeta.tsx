import type { WorkSummary } from '@/api/types'
import { StatusDot } from '@/components/shared/status-dot'
import { formatDuration } from '@/lib/format'
import { useT } from '@/lib/i18n'

export interface ThreadMetaProps {
  threadId: string
  work?: WorkSummary
  // Display names by member id.
  names: ReadonlyMap<string, string>
  // What a person asked, which began the piece of work.
  ask?: string
  onOpenThread?: (threadId: string) => void
}

// The line under a topic's title (docs/webui.md §4.2). Where a person's ask
// began a piece of work: how all of it stands, in whichever topics, and who
// took part. In a topic a member was woken into: the piece of work it is
// part of, which opens its topic.
export function ThreadMeta({ threadId, work, names, ask, onOpenThread }: ThreadMetaProps) {
  const t = useT()
  if (!work) return null
  if (work.thread_id !== threadId) {
    const label = t('thread.partOf', { n: work.thread_number ?? '' })
    return (
      <p className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
        {onOpenThread ? (
          <button
            type="button"
            data-opens-panel
            onClick={() => onOpenThread(work.thread_id)}
            className="flex-none rounded-sm font-medium text-foreground/80 outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50"
          >
            {label}
          </button>
        ) : (
          <span className="flex-none">{label}</span>
        )}
        {ask ? <span className="min-w-0 truncate">· {ask}</span> : null}
      </p>
    )
  }
  const members = (work.members ?? []).flatMap((id) => names.get(id) ?? []).join(t('common.listSeparator'))
  const took = work.ended_at ? t('topic.took', { duration: formatDuration(work.started_at, work.ended_at) }) : ''
  const state = work.running
    ? t('topic.workRunning', { turns: work.turns })
    : work.turns > 1
      ? t('topic.done', { turns: work.turns, took })
      : t('topic.doneOnce', { took })
  return (
    <p className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
      <StatusDot tone={work.running ? 'run' : 'ok'} />
      <span className="flex-none">{state}</span>
      {members ? <span className="min-w-0 truncate">· {members}</span> : null}
    </p>
  )
}
