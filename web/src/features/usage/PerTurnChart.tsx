import { useState } from 'react'
import { Bar, BarChart, CartesianGrid, Cell, LabelList, XAxis, YAxis } from 'recharts'
import type { Usage, UsagePoint } from '@/api/types'
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from '@/components/ui/chart'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { formatCompactCount, formatCount, formatDay, formatHour, formatSpan } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { anchorOf, changedAt, formatSpanTick, peakOf, spanTicks, type Measure } from './usage'

// PerTurnChart is what each turn spent today, or each day a week or a
// month (docs/webui.md 4.20): grey bars from one baseline, the biggest in
// blue with its figure and whose it was written over it. It can show the
// time taken instead. A table gives a screen reader the same numbers.
export function PerTurnChart({ usage }: { usage: Usage }) {
  const t = useT()
  const [measure, setMeasure] = useState<Measure>('tokens')
  const byTurn = usage.step === 'turn'
  const peak = peakOf(usage.points, measure)
  const format = (value: number) => (measure === 'tokens' ? formatCompactCount(value) : formatSpan(value))
  const whenOf = (point: UsagePoint) => (byTurn ? formatHour(point.at) : formatDay(point.at))
  const whoOf = (point: UsagePoint) => (byTurn ? `${point.member ?? ''} · #${point.thread_number ?? ''}` : t('usage.turnCount', { n: point.turns }))
  const data = usage.points.map((point, index) => ({ ...point, value: point[measure], peak: index === peak }))
  const label = measure === 'tokens' ? t('usage.tokens') : t('usage.took')
  const config: ChartConfig = { value: { label, color: 'var(--chart-mark)' } }
  const title = byTurn ? t('usage.perTurn') : t('usage.perDay')
  // Time goes up in whole seconds, minutes or hours; turns that share a
  // minute share its tick.
  const yTicks = measure === 'duration_ms' ? spanTicks(Math.max(0, ...data.map((point) => point.value))) : undefined
  const xTicks = changedAt(usage.points, whenOf).map((point) => point.at)

  return (
    <section aria-label={title} className="flex min-w-0 flex-col gap-3.5 rounded-xl border bg-card px-4.5 pt-3.5 pb-4">
      <div className="flex h-6.5 items-center gap-3">
        <h2 className="grow text-[0.8125rem] font-medium">{title}</h2>
        <Tabs value={measure} onValueChange={(value) => setMeasure(value as Measure)}>
          <TabsList aria-label={t('usage.measure')} className="h-7">
            <TabsTrigger value="tokens" className="px-2 text-xs">
              {t('usage.tokens')}
            </TabsTrigger>
            <TabsTrigger value="duration_ms" className="px-2 text-xs">
              {t('usage.took')}
            </TabsTrigger>
          </TabsList>
        </Tabs>
      </div>
      <ChartContainer config={config} className="aspect-auto h-52 w-full">
        <BarChart data={data} margin={{ top: 34, left: 0, right: 4 }}>
          <CartesianGrid vertical={false} />
          <YAxis
            width={48}
            tickLine={false}
            axisLine={false}
            ticks={yTicks}
            domain={yTicks ? [0, yTicks[yTicks.length - 1]] : undefined}
            tickFormatter={(value: number) => (yTicks ? formatSpanTick(value) : format(value))}
            allowDecimals={false}
          />
          <XAxis
            dataKey="at"
            tickLine={false}
            axisLine={false}
            tickMargin={8}
            minTickGap={32}
            ticks={xTicks}
            tickFormatter={(at: string) => (byTurn ? formatHour(at) : formatDay(at))}
          />
          <ChartTooltip
            cursor={false}
            content={
              <ChartTooltipContent
                labelFormatter={(_, payload) => {
                  const point = payload?.[0]?.payload as UsagePoint | undefined
                  return point ? `${whenOf(point)} · ${whoOf(point)}` : ''
                }}
                formatter={(value) => (
                  <span className="flex w-full justify-between gap-3">
                    <span className="text-muted-foreground">{label}</span>
                    <span className="font-mono font-medium tabular-nums">{format(Number(value))}</span>
                  </span>
                )}
              />
            }
          />
          <Bar dataKey="value" radius={[4, 4, 0, 0]} maxBarSize={14} minPointSize={(value) => (value ? 2 : 0)} isAnimationActive={false}>
            {data.map((point, index) => (
              <Cell key={index} fill={point.peak ? 'var(--chart-accent)' : 'var(--chart-mark)'} />
            ))}
            {/* Keyed by the point, not the label's index: an empty bar has
                no rectangle and no label, and the indices after it shift. */}
            <LabelList
              dataKey="peak"
              content={(props) => {
                const { x = 0, y = 0, width = 0, value } = props as { x?: number; y?: number; width?: number; value?: unknown }
                if (value !== true) return null
                const anchor = anchorOf(peak, data.length)
                const lx = anchor === 'start' ? Number(x) : anchor === 'end' ? Number(x) + Number(width) : Number(x) + Number(width) / 2
                return (
                  <g>
                    <text x={lx} y={Number(y) - 20} textAnchor={anchor} className="fill-foreground text-[0.8125rem] font-semibold">
                      {format(data[peak].value)}
                    </text>
                    <text x={lx} y={Number(y) - 7} textAnchor={anchor} className="fill-subtle text-[0.6875rem]">
                      {whoOf(data[peak])}
                    </text>
                  </g>
                )
              }}
            />
          </Bar>
        </BarChart>
      </ChartContainer>
      <table className="sr-only">
        <caption>{title}</caption>
        <thead>
          <tr>
            <th scope="col">{t('usage.when')}</th>
            {byTurn ? <th scope="col">{t('usage.who')}</th> : null}
            <th scope="col">{t('usage.tokens')}</th>
            <th scope="col">{t('usage.took')}</th>
          </tr>
        </thead>
        <tbody>
          {usage.points.map((point) => (
            <tr key={point.turn_id ?? point.at}>
              <th scope="row">{whenOf(point)}</th>
              {byTurn ? <td>{whoOf(point)}</td> : null}
              <td>{formatCount(point.tokens)}</td>
              <td>{formatSpan(point.duration_ms)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}
