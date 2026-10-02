import type { ReactNode } from 'react'
import { useSearchParams } from 'react-router'
import { errorText } from '@/api/errorText'
import { useProjects } from '@/api/projects'
import type { Usage, UsageRange } from '@/api/types'
import { useUsage } from '@/api/work'
import { Panel } from '@/components/layout/Panel'
import { PanelHeader } from '@/components/layout/PanelHeader'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useWidthRem } from '@/features/rooms/panelLayout'
import { formatCompactCount } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { totalTokens } from '@/lib/tokens'
import { useDocumentTitle } from '@/lib/useDocumentTitle'
import { Members, Runtimes, Works } from './Breakdowns'
import { Meter, Sparkline, Strip } from './Glyphs'
import { PerTurnChart } from './PerTurnChart'
import { cacheShare, cumulative } from './usage'

const ranges: UsageRange[] = ['today', '7d', '30d']

// A Select cannot hold the empty string, which stands for every project.
const allProjects = 'all'
// How wide the top bar must be, measured in English, whose words run
// longest, for the ranges side by side beside the title and the project;
// narrower, a phone's, the range is picked from a menu like the project.
const RANGES_ROW_REM = 26

// UsagePage is where the tokens went, across every machine (docs/webui.md
// 4.20): four figures, what each turn or day spent, the runtimes' shares,
// and the members and pieces of work that spent the most. The range and
// the project are in the address.
export function UsagePage() {
  const t = useT()
  useDocumentTitle(t('usage.title'))
  const [params, setParams] = useSearchParams()
  const range = (ranges as string[]).includes(params.get('range') ?? '') ? (params.get('range') as UsageRange) : 'today'
  const project = params.get('project') ?? ''
  const usage = useUsage(range, project)
  const projects = useProjects()
  const [panelRef, panelRem] = useWidthRem()
  const rangesInMenu = panelRem > 0 && panelRem < RANGES_ROW_REM

  function set(key: string, value: string, fallback: string) {
    setParams((prev) => {
      const next = new URLSearchParams(prev)
      if (value === fallback) next.delete(key)
      else next.set(key, value)
      return next
    })
  }

  return (
    <Panel ref={panelRef}>
      <PanelHeader
        title={t('usage.title')}
        trailing={
          // On a narrow screen the project gives way first, then the title.
          <div className="flex min-w-0 shrink-[100] items-center gap-2 sm:gap-2.5">
            {rangesInMenu ? (
              <Select value={range} onValueChange={(value) => set('range', value, 'today')}>
                <SelectTrigger size="sm" aria-label={t('usage.range')} className="flex-none px-2.5 text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent align="end">
                  {ranges.map((r) => (
                    <SelectItem key={r} value={r}>
                      {t(`usage.range.${r}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : (
              <Tabs value={range} onValueChange={(value) => set('range', value, 'today')} className="flex-none">
                <TabsList aria-label={t('usage.range')} className="h-8">
                  {ranges.map((r) => (
                    <TabsTrigger key={r} value={r} className="px-2 text-xs sm:px-2.5">
                      {t(`usage.range.${r}`)}
                    </TabsTrigger>
                  ))}
                </TabsList>
              </Tabs>
            )}
            <Select value={project || allProjects} onValueChange={(value) => set('project', value === allProjects ? '' : value, '')}>
              <SelectTrigger
                size="sm"
                aria-label={t('usage.project')}
                className="min-w-0 px-2.5 text-xs *:data-[slot=select-value]:block *:data-[slot=select-value]:truncate"
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent align="end">
                <SelectItem value={allProjects}>{t('usage.allProjects')}</SelectItem>
                {(projects.data ?? []).map((p) => (
                  <SelectItem key={p.id} value={p.id}>
                    {p.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        }
      />
      {usage.isPending ? (
        <div role="status" aria-label={t('common.loading')} className="grid grid-cols-4 gap-3.5 px-5 pt-4">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-24 rounded-xl" />
          ))}
        </div>
      ) : usage.isError ? (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>{t('usage.failed')}</EmptyTitle>
            <EmptyDescription>{errorText(usage.error)}</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : usage.data.turns === 0 ? (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>{t('usage.none')}</EmptyTitle>
          </EmptyHeader>
        </Empty>
      ) : (
        <Body usage={usage.data} projects={project === '' && (projects.data?.length ?? 0) > 1} />
      )}
    </Panel>
  )
}

function Body({ usage, projects }: { usage: Usage; projects: boolean }) {
  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <div className="flex flex-col gap-3.5 px-5 pt-1 pb-5">
        <Figures usage={usage} />
        <div className="grid gap-3.5 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
          <PerTurnChart usage={usage} />
          <Runtimes usage={usage} />
        </div>
        <div className="grid gap-3.5 lg:grid-cols-2">
          <Members usage={usage} projects={projects} />
          <Works usage={usage} projects={projects} />
        </div>
      </div>
    </div>
  )
}

// Figures is four numbers, a card each, each with a small picture: the
// tokens and how they added up, how much input the cache gave, the output
// and how it went, and the turns.
function Figures({ usage }: { usage: Usage }) {
  const t = useT()
  const share = cacheShare(usage)
  return (
    <div role="group" aria-label={t('usage.overview')} className="grid grid-cols-2 gap-3.5 md:grid-cols-4">
      <Figure label={t('usage.tokens')} value={formatCompactCount(totalTokens(usage.total))} glyph={<Sparkline values={[0, ...cumulative(usage.points)]} />} />
      <Figure label={t('usage.cacheHit')} value={`${Math.round(share * 100)}%`} glyph={<Meter share={share} />} />
      <Figure
        label={t('usage.output')}
        value={formatCompactCount(usage.total.output_tokens)}
        glyph={<Sparkline values={usage.points.map((p) => p.output)} />}
      />
      <Figure label={t('usage.turns')} value={String(usage.turns)} glyph={<Strip count={usage.turns} />} />
    </div>
  )
}

function Figure({ label, value, glyph }: { label: string; value: string; glyph: ReactNode }) {
  return (
    <section aria-label={label} className="flex flex-col gap-2 rounded-xl border bg-card px-4.5 pt-3.5 pb-4">
      <span className="text-[0.78125rem] text-muted-foreground">{label}</span>
      <span className="flex items-end justify-between gap-3">
        <span className="shrink-0 text-[1.75rem] leading-none font-semibold tracking-[-0.02em] whitespace-nowrap tabular-nums">{value}</span>
        <span className="flex min-w-0 justify-end pb-0.75">{glyph}</span>
      </span>
    </section>
  )
}
