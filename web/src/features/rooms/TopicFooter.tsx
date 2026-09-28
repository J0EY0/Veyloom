import { type AskKind, waitingKeys } from '@/features/approvals/kinds'
import { MessageSquareIcon } from 'lucide-react'
import type { ThreadSummary } from '@/api/types'
import { StatusDot, type StatusTone } from '@/components/shared/status-dot'
import { formatDuration } from '@/lib/format'
import { cn } from '@/lib/utils'
import { t, useT } from '@/lib/i18n'
import { useNow } from '@/lib/useNow'
import { quietFor, useQuietSince } from '@/features/turns/quiet'
import { turnErrorText } from '@/features/turns/turnError'
import { workState } from '@/features/threads/workState'

export interface TopicFooterProps {
  summary: ThreadSummary
  onOpen: () => void
  // The tool the running turn is in right now, from the live stream.
  liveTool?: string
  // The runtime is compacting the session of the turn in flight.
  compacting?: boolean
  // What the topic's turn waits on a person for, if anything.
  waiting?: Waiting
  // Whose turns the topic has when it is not whoever said the root: the
  // member the root woke.
  worker?: string
  // The topic is the one open beside the chat.
  selected?: boolean
  // The room, to find out whether the topic's running turn went quiet
  // (docs/design.md 5.23.8).
  roomId?: string
}

const tones: Record<StatusTone, string> = {
  idle: 'border-border text-muted-foreground',
  ok: 'border-border text-muted-foreground',
  run: 'border-status-run/35 text-foreground',
  wait: 'border-status-wait/40 bg-status-wait/8 text-status-wait',
  fail: 'border-status-fail/40 bg-status-fail/8 text-status-fail',
}

// What hangs under a topic root (docs/webui.md §4.1): how its work stands,
// the whole piece of work where a person's ask began it, and the topic by
// its number with how much was said. Either opens the topic.
export function TopicFooter({ summary, onOpen, liveTool, waiting, compacting, worker, selected, roomId = '' }: TopicFooterProps) {
  const t = useT()
  const running = summary.last_turn?.status === 'running' ? summary.last_turn.id : undefined
  const quietSince = useQuietSince(roomId, running)
  // Minutes are what it says: a clock of its own, the row it hangs under
  // being drawn again only when its message changes.
  const now = useNow(quietSince !== undefined, 30_000)
  const state = topicState(summary, liveTool, waiting, compacting, quietSince ? quietFor(t, quietSince, now) : undefined)
  return (
    <button
      type="button"
      data-opens-panel
      onClick={onOpen}
      className="group/footer mt-2 flex max-w-full items-center gap-1.5 rounded-full text-left outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
    >
      <span className={cn('inline-flex h-6 min-w-0 items-center gap-[0.4375rem] rounded-full border px-2.5 text-xs', tones[state.tone])}>
        <StatusDot tone={state.tone} />
        <span className="truncate">{worker && state.ofTurn ? `${worker} ${state.text}` : state.text}</span>
      </span>
      <span
        className={cn(
          'inline-flex h-6 flex-none items-center gap-1.5 rounded-full px-2.5 text-xs tabular-nums transition-colors',
          selected ? 'bg-selection text-foreground' : 'text-muted-foreground group-hover/footer:bg-accent group-hover/footer:text-foreground',
        )}
      >
        <MessageSquareIcon aria-hidden="true" className="size-3.5" />
        {summary.number ? `#${summary.number} · ` : ''}
        {t('topic.replies', { n: summary.reply_count })}
      </span>
    </button>
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
  // The phrase is about the topic's last turn alone, whose member it can
  // name, rather than about several turns or a piece of work.
  ofTurn?: boolean
}

// topicState reads the summary as one phrase; the same words appear in
// the topic panel's turn headers. quiet says for how long a running turn
// has shown no sign of life, once it went quiet.
export function topicState(summary: ThreadSummary, liveTool?: string, waiting?: Waiting, compacting?: boolean, quiet?: string): TopicState {
  const turn = summary.last_turn
  if (!turn) return { tone: 'idle', text: t('topic.plain') }
  const work = summary.work
  if (turn.status === 'running') {
    if (waiting) return { tone: 'wait', text: t(waitingKeys[waiting.kind].topic, { what: waiting.what }), ofTurn: true }
    const doing = liveTool ? t('topic.running', { tool: liveTool }) : t('topic.working')
    // A compaction that long may be stuck as well.
    if (quiet) return { tone: 'wait', text: `${compacting ? t('turn.compacting') : doing} · ${quiet}`, ofTurn: true }
    if (compacting) return { tone: 'run', text: t('turn.compacting'), ofTurn: true }
    return { tone: 'run', text: doing, ofTurn: true }
  }
  // The topic's own turn is over, and others of its piece of work run on.
  if (work?.running) return { tone: 'run', text: t('topic.workRunning', { turns: work.turns }) }
  switch (turn.status) {
    case 'failed':
      return { tone: 'fail', text: t('topic.failed', { error: turn.error ? turnErrorText(turn.error) : t('topic.unknownError') }), ofTurn: true }
    case 'cancelled':
      return { tone: 'idle', text: t('topic.cancelled'), ofTurn: true }
    default: {
      // A piece of work counts from its first turn's start to its last
      // one's end, in whichever topics they were, and ended as the last of
      // them did, here or in another topic.
      if (work && work.turns > 1) return workState(work, t)
      const [from, to, turns] = work ? [work.started_at, work.ended_at, work.turns] : [turn.started_at, turn.ended_at, summary.turns]
      const took = to ? t('topic.took', { duration: formatDuration(from, to) }) : ''
      return turns > 1 ? { tone: 'ok', text: t('topic.done', { turns, took }) } : { tone: 'ok', text: t('topic.doneOnce', { took }), ofTurn: true }
    }
  }
}
