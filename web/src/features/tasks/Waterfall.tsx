import type { Work, WorkTurn } from '@/api/types'
import { AgentAvatar } from '@/components/shared/agent-avatar'
import { UserAvatar } from '@/components/shared/user-avatar'
import type { AgentLook } from '@/lib/agentLooks'
import { formatCompactCount, formatHour, formatSpan } from '@/lib/format'
import { useWidthRem } from '@/features/rooms/panelLayout'
import { useT } from '@/lib/i18n'
import { totalTokens } from '@/lib/tokens'
import { cn } from '@/lib/utils'
import { at, scaleOf, time, tookMs, turnLabel, withSeconds, type Scale } from './work'

export interface WaterfallProps {
  work: Work
  names: ReadonlyMap<string, string>
  looks: ReadonlyMap<string, AgentLook>
  now: number
}

// Who and what on the left, the turn in the middle, three figures on the
// right: the same columns on every row, the ticks and the grid behind.
const columns = 'grid grid-cols-[18.75rem_minmax(0,1fr)_4.75rem_4.75rem_4rem] items-center gap-x-4'

// How much of the time column a tick's label takes, in rem, with room to
// the next: five characters of the 0.71875rem mono type, eight with seconds.
function labelRem(seconds: boolean): number {
  return (seconds ? 8 : 5) * 0.6 * 0.71875 + 0.75
}

// Waterfall draws a piece of work's turns the way Datadog and Vercel draw a
// trace (docs/webui.md 4.20): a row a turn, its bar at its real time,
// orange where it waited on a person, no words on the bars; how long it
// took, how long it waited and what it spent on the right. A turn that
// only handed work on is a quieter row. Ticks mark the minutes.
export function Waterfall({ work, names, looks, now }: WaterfallProps) {
  const t = useT()
  const scale = scaleOf(work, now)
  const seconds = withSeconds(scale)
  // Each label centred on its tick, and as many as the column holds side
  // by side: every tick's, or every other one's, and so on, when it is
  // narrow. Not measured yet, every tick's.
  const [axisRef, axisRem] = useWidthRem()
  const apart = axisRem / Math.max(scale.ticks.length - 1, 1)
  const every = axisRem > 0 ? Math.max(1, Math.ceil(labelRem(seconds) / apart)) : 1
  return (
    <div role="table" aria-label={t('work.turns')} className="relative">
      <div role="row" className={cn(columns, 'h-5 text-[0.71875rem] text-subtle')}>
        <span role="columnheader">
          <span className="sr-only">{t('work.who')}</span>
        </span>
        <span role="columnheader" ref={axisRef} className="relative h-full">
          <span className="sr-only">{t('work.when')}</span>
          {scale.ticks.map((tick, index) =>
            index % every === 0 ? (
              <span
                key={tick}
                aria-hidden="true"
                className="absolute -translate-x-1/2 font-mono whitespace-nowrap tabular-nums"
                style={{ left: `${at(scale, tick)}%` }}
              >
                {tickLabel(tick, seconds)}
              </span>
            ) : null,
          )}
        </span>
        <span role="columnheader" className="text-right">
          {t('work.took')}
        </span>
        <span role="columnheader" className="text-right">
          {t('work.waited')}
        </span>
        <span role="columnheader" className="text-right">
          {t('work.tokens')}
        </span>
      </div>
      <div className="relative mt-1.5">
        <Grid scale={scale} />
        {work.turns.map((turn) => (
          <Row
            key={turn.id}
            turn={turn}
            scale={scale}
            name={names.get(turn.member_id) ?? t('sender.unknownMember')}
            look={looks.get(turn.member_id)}
            now={now}
            names={names}
          />
        ))}
      </div>
    </div>
  )
}

// tickLabel is a tick's clock time, with its seconds when they differ.
function tickLabel(tick: number, seconds: boolean): string {
  const d = new Date(tick)
  const two = (n: number) => String(n).padStart(2, '0')
  return seconds ? `${two(d.getHours())}:${two(d.getMinutes())}:${two(d.getSeconds())}` : formatHour(d.toISOString())
}

// Grid is the ticks' lines, behind the rows' bars.
function Grid({ scale }: { scale: Scale }) {
  return (
    <div aria-hidden="true" className={cn(columns, 'pointer-events-none absolute inset-0')}>
      <span />
      <span className="relative h-full">
        {scale.ticks.map((tick) => (
          <span key={tick} className="absolute top-0 bottom-0 w-px bg-border/70" style={{ left: `${at(scale, tick)}%` }} />
        ))}
      </span>
    </div>
  )
}

interface RowProps {
  turn: WorkTurn
  scale: Scale
  name: string
  look?: AgentLook
  names: ReadonlyMap<string, string>
  now: number
}

function Row({ turn, scale, name, look, names, now }: RowProps) {
  const t = useT()
  const quiet = turn.relay === true
  const start = time(turn.started_at)
  const end = turn.ended_at ? time(turn.ended_at) : now
  const left = at(scale, start)
  const width = Math.max(at(scale, end) - left, 0.4)
  return (
    <div role="row" className={cn(columns, 'relative', quiet ? 'h-8.5' : 'h-10.5')}>
      <div role="rowheader" className="flex min-w-0 items-center gap-2.25">
        {look ? <AgentAvatar look={look} name={name} size={quiet ? 'xs' : 'sm'} mark={false} /> : <UserAvatar name={name} size={quiet ? 'xs' : 'sm'} />}
        <span className={cn('flex-none', quiet ? 'text-[0.78125rem] text-muted-foreground' : 'text-[0.8125rem] font-medium')}>{name}</span>
        <span className={cn('min-w-0 truncate', quiet ? 'text-[0.78125rem] text-subtle' : 'text-[0.8125rem] text-muted-foreground')}>
          {turnLabel(turn, names)}
        </span>
        {quiet ? null : <span className="flex-none font-mono text-[0.6875rem] text-subtle">#{turn.thread_number}</span>}
      </div>
      <div role="cell" className="relative h-full">
        <span className="sr-only">
          {formatHour(turn.started_at)}–{turn.ended_at ? formatHour(turn.ended_at) : t('work.now')}
        </span>
        <span
          aria-hidden="true"
          className={cn(
            'absolute top-1/2 h-3.5 min-w-1 -translate-y-1/2 overflow-hidden rounded-[0.1875rem] bg-work-bar',
            turn.status === 'failed' && 'bg-status-fail',
          )}
          style={{ left: `${left}%`, width: `${width}%` }}
        >
          {(turn.waits ?? []).map((wait) => {
            const from = Math.max(time(wait.from), start)
            const to = Math.min(wait.to ? time(wait.to) : now, end)
            const span = end - start || 1
            return (
              <span
                key={wait.from}
                className="absolute inset-y-0 min-w-0.75 bg-wait-bar"
                style={{ left: `${((from - start) / span) * 100}%`, width: `${(Math.max(to - from, 0) / span) * 100}%` }}
              />
            )
          })}
        </span>
      </div>
      <span role="cell" className={cn('text-right text-[0.78125rem] tabular-nums', quiet && 'text-subtle')}>
        {formatSpan(tookMs(turn, now))}
      </span>
      <span role="cell" className="text-right text-[0.78125rem] tabular-nums">
        {turn.waited_ms > 0 ? (
          <span className="inline-flex items-center gap-1.25 text-status-wait">
            <span aria-hidden="true" className="size-1.5 rounded-full bg-status-wait" />
            {formatSpan(turn.waited_ms)}
          </span>
        ) : (
          <span className="text-subtle">—</span>
        )}
      </span>
      <span role="cell" className={cn('text-right text-[0.78125rem] tabular-nums', quiet && 'text-subtle')}>
        {formatCompactCount(totalTokens(turn.usage))}
      </span>
    </div>
  )
}
