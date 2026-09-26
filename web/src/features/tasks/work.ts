import type { Work, WorkTurn } from '@/api/types'
import { t } from '@/lib/i18n'

const SECOND = 1000
const MINUTE = 60 * SECOND

// Steps a waterfall's ticks may go in: the first that keeps them to a
// handful. A piece of work may take seconds or hours.
const steps = [1, 2, 5, 10, 15, 30].map((s) => s * SECOND).concat([1, 2, 5, 10, 15, 30, 60, 120, 240, 480, 1440].map((m) => m * MINUTE))

// Scale is where a waterfall's time runs, from and to, with its ticks and
// how far apart they are.
export interface Scale {
  from: number
  to: number
  ticks: number[]
  step: number
}

// scaleOf is the stretch a piece of work's turns cover, out to whole ticks
// on either side: from its first turn's start to its last one's end, or to
// now while one runs.
export function scaleOf(work: Work, now: number): Scale {
  const starts = work.turns.map((turn) => time(turn.started_at))
  const ends = work.turns.map((turn) => (turn.ended_at ? time(turn.ended_at) : now))
  const first = Math.min(...starts)
  const last = Math.max(first + 1, ...ends)
  // Counted out to whole ticks on either side; a clock with seconds is
  // wider, and fewer of them fit.
  const step = steps.find((s) => Math.ceil(last / s) - Math.floor(first / s) <= (s < MINUTE ? 4 : 8)) ?? steps[steps.length - 1]
  const from = Math.floor(first / step) * step
  const to = Math.max(from + step, Math.ceil(last / step) * step)
  const ticks: number[] = []
  for (let tick = from; tick <= to; tick += step) ticks.push(tick)
  return { from, to, ticks, step }
}

// withSeconds says whether a scale's ticks need their seconds.
export function withSeconds(scale: Scale): boolean {
  return scale.step < MINUTE
}

// at is where a moment falls on a scale, as a share of it, 0 to 100.
export function at(scale: Scale, moment: number): number {
  return Math.min(100, Math.max(0, ((moment - scale.from) / (scale.to - scale.from)) * 100))
}

export function time(iso: string): number {
  return new Date(iso).getTime()
}

// turnLabel says what a turn of a piece of work did: began its task (the
// task's title); split the work up; handed it on to members; summed up
// what came of it; went on, woken again.
export function turnLabel(turn: WorkTurn, names: ReadonlyMap<string, string>): string {
  const who = (turn.woke ?? [])
    .map((id) => names.get(id) ?? '')
    .filter(Boolean)
    .join(t('common.listSeparator'))
  switch (turn.kind) {
    case 'split':
      return t('work.split')
    case 'handoff':
      return t('work.handOff', { names: who })
    case 'sumup':
      return t('work.sumUp')
    case 'continue':
      return turn.title || t('work.continue')
    default:
      return turn.title || t('tasks.untitled', { n: turn.thread_number })
  }
}

// tookMs is how long a turn took, or has so far.
export function tookMs(turn: WorkTurn, now: number): number {
  return (turn.ended_at ? time(turn.ended_at) : now) - time(turn.started_at)
}
