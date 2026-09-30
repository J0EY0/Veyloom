import { useState } from 'react'
import { ChevronRightIcon, FileTextIcon, InfoIcon, MessageSquarePlusIcon, ShieldCheckIcon, TerminalIcon } from 'lucide-react'
import { Task, TaskContent, TaskItem, TaskItemFile, TaskTrigger } from '@/components/ai-elements/task'
import { StatusDot } from '@/components/shared/status-dot'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { describeTool, summarize, type ActivityItem } from './activity'
import { useT } from '@/lib/i18n'

export interface ActivityBlockProps {
  items: ActivityItem[]
  // How long the turn took, once it has ended.
  duration?: string
  // Opens the turn's full record.
  onOpen?: () => void
  // The turn is under way: its latest steps stay in sight, folded or not.
  live?: boolean
}

// liveSteps is how many of a running turn's latest steps are in sight.
const liveSteps = 3

const approvalKeys = ['pending', 'allowed', 'denied', 'expired', 'cancelled'] as const

// How a call's state reads, by colour.
const toolTones = { running: 'text-status-run', waiting: 'text-status-wait', done: 'text-subtle', failed: 'text-status-fail' } as const

// A turn's tool activity folded into one quiet line of its answer, as an
// AI Elements task: what it did and how long it took. Opened, one row per
// file or command with its outcome, and the way to the turn's full record
// (docs/webui.md §4.2).
export function ActivityBlock({ items, duration, onOpen, live }: ActivityBlockProps) {
  const t = useT()
  const [open, setOpen] = useState(false)
  if (items.length === 0) return null
  const summary = [t('activity.process'), summarize(items), duration].filter(Boolean).join(' · ')
  const latest = live && !open ? items.slice(-liveSteps) : []
  return (
    <Task open={open} onOpenChange={setOpen} className="mt-0.5 mb-1 text-xs">
      <TaskTrigger title={summary}>
        <button type="button" className="flex max-w-full items-center gap-1 rounded-sm text-subtle transition-colors hover:text-muted-foreground">
          <ChevronRightIcon className="size-3.5 flex-none transition-transform duration-200 group-data-[state=open]:rotate-90" />
          <span className="truncate">{summary}</span>
        </button>
      </TaskTrigger>
      {latest.length > 0 ? (
        // While the turn runs, what it is doing now, without opening.
        <div className="mt-1 rounded-lg bg-muted px-3 py-1.5">
          {items.length > latest.length ? (
            <button type="button" onClick={() => setOpen(true)} className="flex h-6 items-center text-[0.75rem] text-subtle hover:text-muted-foreground">
              {t('activity.earlier', { n: items.length - latest.length })}
            </button>
          ) : null}
          {latest.map((item, index) => (
            <div key={index} className="flex h-6 items-center gap-2 text-[0.75rem] text-muted-foreground">
              <Row item={item} />
            </div>
          ))}
        </div>
      ) : null}
      <TaskContent className="[&>div]:mt-1 [&>div]:space-y-0 [&>div]:rounded-lg [&>div]:border-l-0 [&>div]:bg-muted [&>div]:px-3 [&>div]:py-1.5">
        {items.map((item, index) => (
          <TaskItem key={index} className="flex h-6 items-center gap-2 text-[0.75rem] text-muted-foreground">
            <Row item={item} />
          </TaskItem>
        ))}
        {onOpen ? (
          <Button variant="link" size="xs" className="h-6 px-0 text-xs font-normal text-muted-foreground" onClick={onOpen}>
            {t('activity.open')}
          </Button>
        ) : null}
      </TaskContent>
    </Task>
  )
}

function Row({ item }: { item: ActivityItem }) {
  const t = useT()
  switch (item.kind) {
    case 'file':
      return (
        <TaskItemFile className="h-5 max-w-full border-0 bg-secondary px-1.5 font-mono text-[0.71875rem]" translate="no">
          <FileTextIcon className="size-3 flex-none text-subtle" />
          <span className="min-w-0 truncate">{item.path}</span>
        </TaskItemFile>
      )
    case 'tool':
      return (
        <>
          <TerminalIcon className="size-3.5 flex-none text-subtle" />
          <span className="min-w-0 truncate font-mono" translate="no">
            {describeTool(item.tool, item.input)}
          </span>
          <span className={cn('ml-auto flex-none text-xs', toolTones[item.status])}>{t(`activity.${item.status}`)}</span>
        </>
      )
    case 'approval':
      return (
        <>
          <ShieldCheckIcon className="size-3.5 flex-none text-subtle" />
          <span className="min-w-0 truncate">
            {t('activity.requests')} <span className="font-mono">{describeTool(item.tool, item.input)}</span>
          </span>
          <span className={cn('ml-auto flex-none text-xs', item.status === 'pending' ? 'text-status-wait' : 'text-subtle')}>
            {(approvalKeys as readonly string[]).includes(item.status) ? t(`activity.${item.status as (typeof approvalKeys)[number]}`) : item.status}
          </span>
        </>
      )
    case 'steer':
      return (
        <>
          <MessageSquarePlusIcon className="size-3.5 flex-none text-subtle" />
          <span className="min-w-0 truncate">{t('activity.steer', { who: item.who, text: item.text })}</span>
        </>
      )
    case 'notice':
      // The hub's own steps of getting the worktree ready (activity.ts).
      return (
        <>
          <InfoIcon className="size-3.5 flex-none text-subtle" />
          <span className="min-w-0 truncate">{item.text}</span>
        </>
      )
    default:
      return (
        <>
          <StatusDot tone="fail" />
          <span className="min-w-0 truncate text-status-fail">{item.text}</span>
        </>
      )
  }
}
