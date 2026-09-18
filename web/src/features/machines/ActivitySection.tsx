import { useId, useState, type ReactNode } from 'react'
import { useMachineActivity } from '@/api/agents'
import type { ActivityRange, MachineActivity } from '@/api/types'
import { TokenCount } from '@/components/shared/token-count'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { runtimeName } from '@/lib/runtimes'
import { formatCount, formatSpan } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { totalTokens } from '@/lib/tokens'
import { ActivityChart } from './ActivityChart'

const ranges: ActivityRange[] = ['24h', '7d', '30d']

// A Select cannot hold the empty string, which stands for no choice.
const allRuntimes = 'all'

export interface ActivitySectionProps {
  machineId: string
  // The runtimes found on the machine, to pick one's turns by.
  runtimes: string[]
}

// What this machine did, as two bar charts side by side, one above the
// other when the detail is narrow: the turns, the failed ones in the
// failure colour, and the tokens they spent. Each card picks its own range
// in its top right corner; the turns card can also keep one runtime's. Two
// charts rather than one: a count of turns and a count of tokens share no
// scale. Side by side, the cards share their rows, so a header whose
// controls had to wrap does not push its chart below the other.
export function ActivitySection({ machineId, runtimes }: ActivitySectionProps) {
  return (
    <div className="grid gap-x-4 gap-y-4 @xl:grid-cols-2">
      <TurnsCard machineId={machineId} runtimes={runtimes} />
      <TokensCard machineId={machineId} />
    </div>
  )
}

function TurnsCard({ machineId, runtimes }: ActivitySectionProps) {
  const t = useT()
  const [range, setRange] = useState<ActivityRange>('24h')
  const [runtime, setRuntime] = useState(allRuntimes)
  const activity = useMachineActivity(machineId, { range, runtime: runtime === allRuntimes ? '' : runtime })
  const title = t('machines.legendTurns')
  return (
    <ActivityCard
      title={title}
      loading={activity.isPending}
      summary={activity.data ? turnSummary(activity.data, t) : null}
      error={activity.error?.message}
      actions={
        <>
          <Select value={runtime} onValueChange={setRuntime}>
            <SelectTrigger size="sm" aria-label={t('machines.runtimeFilter')} className="px-2.5 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={allRuntimes}>{t('machines.runtimeAll')}</SelectItem>
              {runtimes.map((name) => (
                <SelectItem key={name} value={name}>
                  {runtimeName(name)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <RangeTabs value={range} onChange={setRange} />
        </>
      }
    >
      {activity.data ? (
        <ActivityChart
          activity={activity.data}
          caption={`${title} · ${t(`machines.period.${activity.data.range}`)}`}
          series={[
            { key: 'turns', label: title, color: 'var(--primary)' },
            { key: 'failed', label: t('machines.legendFailed'), color: 'var(--status-fail)' },
          ]}
        />
      ) : null}
    </ActivityCard>
  )
}

function TokensCard({ machineId }: { machineId: string }) {
  const t = useT()
  const [range, setRange] = useState<ActivityRange>('24h')
  const activity = useMachineActivity(machineId, { range })
  const title = t('machines.legendTokens')
  return (
    <ActivityCard
      title={title}
      loading={activity.isPending}
      summary={activity.data ? tokenSummary(activity.data) : null}
      error={activity.error?.message}
      actions={<RangeTabs value={range} onChange={setRange} />}
    >
      {activity.data ? (
        <ActivityChart
          activity={activity.data}
          caption={`${title} · ${t(`machines.period.${activity.data.range}`)}`}
          series={[{ key: 'tokens', label: title, color: 'var(--primary)' }]}
          format={formatCount}
        />
      ) : null}
    </ActivityCard>
  )
}

interface ActivityCardProps {
  title: string
  // Until the first range arrives: placeholders instead of the summary and
  // the bars.
  loading: boolean
  // What the range adds up to; nothing when nothing ran, as the empty bars
  // already say so.
  summary: ReactNode
  error?: string
  // The controls in the top right corner.
  actions: ReactNode
  children: ReactNode
}

// A chart's card, kept short: its name with the controls across from it,
// then what the range adds up to, then the bars. Controls that do not fit
// beside the name go below it.
function ActivityCard({ title, loading, summary, error, actions, children }: ActivityCardProps) {
  const t = useT()
  const id = useId()
  const line = error ? t('machines.activityFailed', { error }) : loading ? <Skeleton className="h-4 w-40 max-w-full" /> : summary
  return (
    <Card role="figure" aria-labelledby={id} className="row-span-2 grid min-w-0 grid-rows-subgrid gap-4 py-4">
      <CardHeader className="flex flex-col gap-2 px-4">
        <div className="flex w-full flex-wrap items-center justify-between gap-2">
          <CardTitle id={id}>{title}</CardTitle>
          <div className="flex items-center gap-2">{actions}</div>
        </div>
        {line ? <CardDescription>{line}</CardDescription> : null}
      </CardHeader>
      <CardContent className="self-end px-2">{loading ? <Skeleton className="h-44 w-full" /> : children}</CardContent>
    </Card>
  )
}

function RangeTabs({ value, onChange }: { value: ActivityRange; onChange: (range: ActivityRange) => void }) {
  const t = useT()
  return (
    <Tabs value={value} onValueChange={(next) => onChange(next as ActivityRange)}>
      <TabsList aria-label={t('machines.range')} className="h-8">
        {ranges.map((range) => (
          <TabsTrigger key={range} value={range} className="px-2 text-xs">
            {t(`machines.range.${range}`)}
          </TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  )
}

function turnSummary(data: MachineActivity, t: ReturnType<typeof useT>): string | null {
  if (data.turns === 0) return null
  const parts = [t('machines.turnCount', { n: data.turns })]
  if (data.failed > 0) parts.push(t('machines.failedCount', { n: data.failed }))
  if (data.median_ms > 0) parts.push(t('machines.medianTook', { duration: formatSpan(data.median_ms) }))
  return parts.join(' · ')
}

// The range's tokens, with what they were made of on hover.
function tokenSummary(data: MachineActivity): ReactNode {
  return totalTokens(data.usage) === 0 ? null : <TokenCount usage={data.usage} />
}
