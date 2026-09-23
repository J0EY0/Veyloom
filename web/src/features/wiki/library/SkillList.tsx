import { Fragment, useMemo, useState } from 'react'
import { BlocksIcon, ChevronDownIcon, ChevronRightIcon, FolderInputIcon } from 'lucide-react'
import { useAgents } from '@/api/agents'
import type { WikiCatalog, WikiTeam } from '@/api/types'
import { PageBody } from '@/components/layout/PageBody'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { DropdownMenu, DropdownMenuContent, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Empty, EmptyContent, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { ItemGroup, ItemSeparator } from '@/components/ui/item'
import { Skeleton } from '@/components/ui/skeleton'
import { useT } from '@/lib/i18n'
import { SearchBox } from './SearchBox'
import { SkillRow } from './SkillRow'
import { filterSkills, noTeam, skillRows, type SkillRow as Row } from './model'

// SkillList is the library's skills as a market lists them (docs/webui.md
// 4.10): a search and a team to narrow by, then a row each, the retired
// ones folded away at the end. With no skill at all there is nothing to
// narrow, only the way to import the first.
export function SkillList({ catalog, onImport }: { catalog?: WikiCatalog; onImport: () => void }) {
  const t = useT()
  const agents = useAgents()
  const [query, setQuery] = useState('')
  const [team, setTeam] = useState('')
  const rows = useMemo(() => skillRows(catalog, agents.data ?? []), [catalog, agents.data])

  if (!catalog) return <ListSkeleton />
  if (rows.length === 0) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <BlocksIcon />
          </EmptyMedia>
          <EmptyTitle>{t('library.empty')}</EmptyTitle>
        </EmptyHeader>
        <EmptyContent>
          <Button size="sm" onClick={onImport}>
            <FolderInputIcon data-icon="inline-start" />
            {t('library.import.open')}
          </Button>
        </EmptyContent>
      </Empty>
    )
  }

  const teams = catalog.teams ?? []
  const filter = { query, team }
  const live = filterSkills(
    rows.filter((row) => !row.retired),
    filter,
  )
  const retired = filterSkills(
    rows.filter((row) => row.retired),
    filter,
  )
  const nothing = live.length === 0 && retired.length === 0
  return (
    <PageBody width="narrow" fill={nothing} className="pt-2">
      <div className="flex flex-wrap items-center gap-2">
        <SearchBox value={query} onChange={setQuery} label={t('library.search')} />
        <TeamFilter rows={rows} teams={teams} value={team} onChange={setTeam} />
      </div>
      {nothing ? (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>{t('library.noMatch')}</EmptyTitle>
          </EmptyHeader>
          <EmptyContent>
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                setQuery('')
                setTeam('')
              }}
            >
              {t('agents.clearFilters')}
            </Button>
          </EmptyContent>
        </Empty>
      ) : (
        <>
          {live.length > 0 ? <Rows rows={live} teams={teams} label={t('library.tab.skills')} /> : null}
          {retired.length > 0 ? (
            <Collapsible defaultOpen={live.length === 0} className="group/retired">
              <CollapsibleTrigger className="flex items-center gap-1 rounded-md px-1 py-1 text-xs font-medium text-subtle hover:text-foreground">
                <ChevronRightIcon className="size-3.5 transition-transform group-data-[state=open]/retired:rotate-90" aria-hidden="true" />
                {t('library.retired')}
                <span className="ml-0.5 tabular-nums">{retired.length}</span>
              </CollapsibleTrigger>
              <CollapsibleContent className="pt-1 opacity-75">
                <Rows rows={retired} teams={teams} label={t('library.retired')} />
              </CollapsibleContent>
            </Collapsible>
          ) : null}
        </>
      )}
    </PageBody>
  )
}

function Rows({ rows, teams, label }: { rows: Row[]; teams: WikiTeam[]; label: string }) {
  return (
    <ItemGroup aria-label={label} className="-mx-3">
      {rows.map((row, index) => (
        <Fragment key={row.path}>
          {index > 0 ? <ItemSeparator className="mx-3 data-[orientation=horizontal]:w-auto" /> : null}
          <SkillRow row={row} teams={teams} />
        </Fragment>
      ))}
    </ItemGroup>
  )
}

// TeamFilter narrows the list to one team's skills, or nobody's. Only the
// teams with skills are offered, and only when there is a choice to make.
function TeamFilter({ rows, teams, value, onChange }: { rows: Row[]; teams: WikiTeam[]; value: string; onChange: (team: string) => void }) {
  const t = useT()
  const owners = [...new Set(rows.map((row) => row.team))]
  if (owners.length < 2) return null
  const nameOf = (slug: string) => (slug === noTeam || slug === '' ? t('skill.unowned') : (teams.find((known) => known.slug === slug)?.name ?? slug))
  const choices = owners
    .map((slug) => (slug === '' ? noTeam : slug))
    .sort((a, b) => (a === noTeam ? 1 : b === noTeam ? -1 : nameOf(a).localeCompare(nameOf(b))))
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm" className="font-normal">
          {t('library.teamIs', { team: value ? nameOf(value) : t('library.allTeams') })}
          <ChevronDownIcon data-icon="inline-end" className="text-subtle" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start">
        <DropdownMenuRadioGroup value={value} onValueChange={onChange}>
          <DropdownMenuRadioItem value="">{t('library.allTeams')}</DropdownMenuRadioItem>
          {choices.map((slug) => (
            <DropdownMenuRadioItem key={slug} value={slug}>
              {nameOf(slug)}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

// Where the rows will be while the library is read.
export function ListSkeleton() {
  const t = useT()
  return (
    <PageBody width="narrow" className="pt-2">
      <div role="status" aria-label={t('common.loading')} className="flex flex-col gap-5 pt-12">
        {[0, 1, 2].map((n) => (
          <div key={n} className="flex gap-3">
            <Skeleton className="size-9 rounded-lg" />
            <div className="flex flex-1 flex-col gap-2">
              <Skeleton className="h-4 w-40" />
              <Skeleton className="h-3 w-full" />
            </div>
          </div>
        ))}
      </div>
    </PageBody>
  )
}
