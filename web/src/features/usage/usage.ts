import type { Usage, UsagePoint } from '@/api/types'
import { formatSpan } from '@/lib/format'
import { t } from '@/lib/i18n'

// What the usage page reads off the figures (docs/webui.md 4.20).

// cacheShare is how much of the input came from a prompt cache: read from
// it, over all the input, fresh, read and written.
export function cacheShare(usage: Usage): number {
  const { input_tokens, cache_read_tokens, cache_write_tokens } = usage.total
  const input = input_tokens + cache_read_tokens + cache_write_tokens
  return input > 0 ? cache_read_tokens / input : 0
}

// cumulative is the tokens spent so far at each point, oldest first.
export function cumulative(points: UsagePoint[]): number[] {
  let sum = 0
  return points.map((point) => (sum += point.tokens))
}

// Measure is what the per-turn chart shows: tokens or time taken.
export type Measure = 'tokens' | 'duration_ms'

// peakOf is the point that spent the most, or took the longest: the one
// the chart is about. -1 when every point is nothing.
export function peakOf(points: UsagePoint[], measure: Measure): number {
  let peak = -1
  points.forEach((point, index) => {
    if (point[measure] > 0 && (peak < 0 || point[measure] > points[peak][measure])) peak = index
  })
  return peak
}

// shareOf is a part of a whole, 0 to 1.
export function shareOf(part: number, whole: number): number {
  return whole > 0 ? part / whole : 0
}

const SECOND = 1000
const MINUTE = 60 * SECOND
const HOUR = 60 * MINUTE

// Steps a duration axis may go in: whole seconds, minutes or hours.
const spanSteps = [1, 2, 5, 10, 15, 30]
  .map((s) => s * SECOND)
  .concat([1, 2, 5, 10, 15, 30].map((m) => m * MINUTE))
  .concat([1, 2, 3, 6, 12, 24, 48, 96, 192].map((h) => h * HOUR))

// spanTicks are a duration axis's ticks up to the longest, in the first
// step that keeps them to four above nought.
export function spanTicks(longest: number): number[] {
  const step = spanSteps.find((s) => Math.ceil(longest / s) <= 4) ?? spanSteps[spanSteps.length - 1]
  const ticks: number[] = []
  for (let tick = 0; tick <= Math.max(step, Math.ceil(longest / step) * step); tick += step) ticks.push(tick)
  return ticks
}

// formatSpanTick is a duration axis's tick: nought bare, whole hours as
// hours.
export function formatSpanTick(ms: number): string {
  if (ms === 0) return '0'
  return ms >= HOUR && ms % HOUR === 0 ? t('duration.hours', { n: ms / HOUR }) : formatSpan(ms)
}

// changedAt are the points whose label differs from the one before: an
// axis's ticks when many turns share a minute.
export function changedAt<T>(points: T[], label: (point: T) => string): T[] {
  return points.filter((point, index) => index === 0 || label(point) !== label(points[index - 1]))
}

// anchorOf is how the label over a chart's bar lines up with it: over its
// middle, or from its edge near either end of the chart, where a centred
// label would run off.
export function anchorOf(index: number, count: number): 'start' | 'middle' | 'end' {
  if (count < 3) return 'middle'
  const at = index / (count - 1)
  return at < 0.15 ? 'start' : at > 0.85 ? 'end' : 'middle'
}
