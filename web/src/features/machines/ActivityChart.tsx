import { useId } from 'react'
import { Bar, BarChart, CartesianGrid, Rectangle, XAxis, YAxis, type BarShapeProps } from 'recharts'
import type { ActivityBucket, MachineActivity } from '@/api/types'
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from '@/components/ui/chart'
import { formatDay, formatHour } from '@/lib/format'
import { useT } from '@/lib/i18n'

const HOUR_MS = 60 * 60 * 1000

// Bars keep their rounded end; a part that fills its whole bar takes it too.
const TOP: [number, number, number, number] = [4, 4, 0, 0]

export interface ActivitySeries {
  key: keyof Pick<ActivityBucket, 'turns' | 'failed' | 'tokens'>
  label: string
  // A CSS colour; theme tokens keep it right in light and dark.
  color: string
}

export interface ActivityChartProps {
  activity: MachineActivity
  // The first is the whole of each bar; any after it are parts of that
  // whole, drawn over it from the baseline up, so hovering reads "3 turns,
  // 1 failed" rather than two stacked remainders.
  series: ActivitySeries[]
  // Names the table a screen reader gets.
  caption: string
  // How a value reads in that table.
  format?: (value: number) => string
}

// A machine's hours or days as bars from one baseline, the way shadcn's bar
// charts draw: horizontal grid lines, no value axis, time along the bottom,
// a highlighted column and every series' number on hover. A table gives a
// screen reader the same numbers.
export function ActivityChart({ activity, series, caption, format = String }: ActivityChartProps) {
  const t = useT()
  const id = useId()
  const config: ChartConfig = Object.fromEntries(series.map(({ key, label, color }) => [key, { label, color }]))
  const hourly = activity.step === 'hour'
  const [whole, ...parts] = series

  return (
    <>
      <ChartContainer config={config} className="aspect-auto h-44 w-full">
        <BarChart data={activity.buckets} margin={{ left: 4, right: 4 }}>
          <CartesianGrid vertical={false} />
          {/* Hidden, it keeps a range with nothing in it at the baseline. */}
          <YAxis hide allowDecimals={false} domain={[0, (max: number) => Math.max(max, 1)]} />
          <XAxis
            dataKey="start"
            tickLine={false}
            axisLine={false}
            tickMargin={8}
            minTickGap={24}
            tickFormatter={(start: string) => (hourly ? formatHour(start) : formatDay(start))}
          />
          {/* The parts sit on an axis of their own, so they draw over the whole instead of beside it. */}
          <XAxis xAxisId={`${id}-parts`} dataKey="start" hide />
          <ChartTooltip content={<ChartTooltipContent indicator="dot" labelFormatter={(start) => bucketLabel(String(start), hourly)} />} />
          <Bar dataKey={whole.key} fill={`var(--color-${whole.key})`} radius={TOP} maxBarSize={32} minPointSize={(value) => (value ? 2 : 0)} />
          {parts.map(({ key }) => (
            <Bar
              key={key}
              dataKey={key}
              xAxisId={`${id}-parts`}
              fill={`var(--color-${key})`}
              maxBarSize={32}
              shape={(bar: BarShapeProps) => <Rectangle {...bar} radius={bar.payload[key] >= bar.payload[whole.key] ? TOP : 0} />}
            />
          ))}
        </BarChart>
      </ChartContainer>
      <table className="sr-only">
        <caption>{caption}</caption>
        <thead>
          <tr>
            <th scope="col">{t('machines.hourColumn')}</th>
            {series.map(({ key, label }) => (
              <th key={key} scope="col">
                {label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {activity.buckets.map((bucket) => (
            <tr key={bucket.start}>
              <th scope="row">{hourly ? `${formatDay(bucket.start)} ${formatHour(bucket.start)}` : formatDay(bucket.start)}</th>
              {series.map(({ key }) => (
                <td key={key}>{format(bucket[key])}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </>
  )
}

// What a bucket is called on hover: its day, and for an hour its span.
function bucketLabel(start: string, hourly: boolean): string {
  if (!hourly) return formatDay(start)
  const end = new Date(new Date(start).getTime() + HOUR_MS).toISOString()
  return `${formatDay(start)} ${formatHour(start)}–${formatHour(end)}`
}
