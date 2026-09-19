import { type AskKind, waitingKeys } from '@/features/approvals/kinds'
import { ReplyIcon } from 'lucide-react'
import type { ThreadSummary } from '@/api/types'
import { StatusDot, type StatusTone } from '@/components/shared/status-dot'
import { Button } from '@/components/ui/button'
import { formatDuration, formatTime } from '@/lib/format'
import { cn } from '@/lib/utils'
import { t, useT } from '@/lib/i18n'

export interface TopicFooterProps {
  summary: ThreadSummary
  onOpen: () => void
  // The tool the running turn is in right now, from the live stream.
  liveTool?: string
  // The runtime is compacting the session of the turn in flight.
  compacting?: boolean
  // What the topic's agent is waiting for a person to approve.
  // What the topic's turn waits on a person for, if anything.
  waiting?: Waiting
}

// The line under a topic root: how the latest turn is doing, how much was
// said, when. Clicking opens the topic.
export function TopicFooter({ summary, onOpen, liveTool, waiting, compacting }: TopicFooterProps) {
  const t = useT()
  const state = topicState(summary, liveTool, waiting, compacting)
  return (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      data-opens-panel
      onClick={onOpen}
      className="mt-1.5 -ml-2 h-7 max-w-full gap-2 px-2 text-[0.78125rem] font-normal text-muted-foreground hover:text-foreground"
    >
      <ReplyIcon className="size-3.5 text-subtle" />
      <span className={cn('inline-flex min-w-0 items-center gap-[0.4375rem] truncate', state.tone === 'wait' && 'text-status-wait')}>
        <StatusDot tone={state.tone} />
        <span className="truncate">{state.text}</span>
      </span>
      <span className="flex-none text-xs text-subtle tabular-nums">
        {summary.number ? `#${summary.number} · ` : ''}
        {t('topic.replies', { n: summary.reply_count })}
        {summary.last_reply_at ? ` · ${formatTime(summary.last_reply_at)}` : ''}
      </span>
    </Button>
  )
}

// Waiting is what a turn waits on a person for: in one line, and of what
// kind, a permission, an answer, a form or a link.
export interface Waiting {
  what: string
  kind: AskKind
}

export interface TopicState {
  tone: StatusTone
  text: string
}

// topicState reads the summary as one phrase; the same words appear in
// the topic panel's turn headers.
export function topicState(summary: ThreadSummary, liveTool?: string, waiting?: Waiting, compacting?: boolean): TopicState {
  const turn = summary.last_turn
  if (!turn) return { tone: 'idle', text: t('topic.plain') }
  switch (turn.status) {
    case 'running':
      if (waiting) return { tone: 'wait', text: t(waitingKeys[waiting.kind].topic, { what: waiting.what }) }
      if (compacting) return { tone: 'run', text: t('turn.compacting') }
      return { tone: 'run', text: liveTool ? t('topic.running', { tool: liveTool }) : t('topic.working') }
    case 'failed':
      return { tone: 'fail', text: t('topic.failed', { error: turn.error || t('topic.unknownError') }) }
    case 'cancelled':
      return { tone: 'idle', text: t('topic.cancelled') }
    default: {
      const took = turn.ended_at ? t('topic.took', { duration: formatDuration(turn.started_at, turn.ended_at) }) : ''
      return { tone: 'ok', text: t('topic.done', { turns: summary.turns, took }) }
    }
  }
}
