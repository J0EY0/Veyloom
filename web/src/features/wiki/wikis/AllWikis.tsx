import { Fragment, useDeferredValue, useMemo, useState } from 'react'
import { BookOpenIcon, ChevronRightIcon, SearchXIcon } from 'lucide-react'
import { Link } from 'react-router'
import type { WikiProjectHit, WikiSummary } from '@/api/types'
import { useWikis, useWikisSearch } from '@/api/wiki'
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Item, ItemActions, ItemContent, ItemDescription, ItemGroup, ItemTitle } from '@/components/ui/item'
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInput,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from '@/components/ui/sidebar'
import { formatTime } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { wikisHref } from '../links'
import { HitLink, ListSkeleton } from '../WikiSidebar'

// AllWikis is the Wiki page of the sidebar with no project chosen
// (docs/design.md 5.18): on the left a search through every project's
// wiki, its hits under the projects whose they are, or else the projects;
// on the right a line about each project's wiki. On a phone the left
// column is the page.
export function AllWikis() {
  const t = useT()
  const wikis = useWikis()
  const [query, setQuery] = useState('')
  const searching = useDeferredValue(query.trim())
  const sorted = useMemo(() => [...(wikis.data ?? [])].sort((a, b) => a.project_name.localeCompare(b.project_name)), [wikis.data])

  // No project, no wiki: nothing to search or list, only the note, in the
  // middle of the page.
  if (wikis.data && sorted.length === 0) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <BookOpenIcon />
          </EmptyMedia>
          <EmptyTitle>{t('wikis.empty')}</EmptyTitle>
        </EmptyHeader>
      </Empty>
    )
  }
  return (
    <div className="flex min-h-0 flex-1">
      <Sidebar collapsible="none" aria-label={t('wikis.projects')} className="w-full md:w-68 md:flex-none md:border-r">
        <SidebarHeader className="px-3 pt-3 pb-1">
          <SidebarInput
            type="search"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t('wikis.search')}
            aria-label={t('wikis.search')}
            className="[&::-webkit-search-cancel-button]:appearance-none"
          />
        </SidebarHeader>
        <SidebarContent>
          {searching !== '' ? (
            <Results query={searching} />
          ) : wikis.isPending ? (
            <ListSkeleton />
          ) : (
            <SidebarGroup className="py-1">
              <SidebarGroupLabel>{t('wikis.projects')}</SidebarGroupLabel>
              <SidebarGroupContent>
                <SidebarMenu>
                  {sorted.map((wiki) => (
                    <SidebarMenuItem key={wiki.project_id}>
                      <SidebarMenuButton asChild size="sm">
                        <Link to={wikisHref(wiki.project_id)}>
                          <span className="truncate">{wiki.project_name}</span>
                          <span className="ml-auto text-subtle tabular-nums">{wiki.pages}</span>
                        </Link>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  ))}
                </SidebarMenu>
              </SidebarGroupContent>
            </SidebarGroup>
          )}
        </SidebarContent>
      </Sidebar>
      <div className="hidden min-w-0 flex-1 overflow-y-auto md:block">
        <div className="mx-auto w-full max-w-[46rem] px-5 pt-6 pb-12 md:px-10">
          <ItemGroup className="gap-2">
            {sorted.map((wiki) => (
              <WikiLine key={wiki.project_id} wiki={wiki} />
            ))}
          </ItemGroup>
        </div>
      </div>
    </div>
  )
}

// WikiLine is one project's wiki: its name, and how many pages, how many
// due to be checked again, and when it last changed.
function WikiLine({ wiki }: { wiki: WikiSummary }) {
  const t = useT()
  // Pages due to be checked again in the wait colour, as the overview has
  // them.
  const facts = [
    { text: t('wiki.pageCount', { n: wiki.pages }) },
    { text: wiki.due > 0 ? t('wiki.reviewCount', { n: wiki.due }) : '', due: true },
    { text: wiki.changed_at ? t('wikis.changedAt', { when: formatTime(wiki.changed_at) }) : '' },
  ].filter((fact) => fact.text !== '')
  return (
    <Item asChild variant="outline" size="sm">
      <Link to={wikisHref(wiki.project_id)}>
        <ItemContent className="min-w-0">
          <ItemTitle className="break-words">{wiki.project_name}</ItemTitle>
          <ItemDescription className="text-xs">
            {facts.map((fact, index) => (
              <Fragment key={index}>
                {index > 0 ? ' · ' : null}
                <span className={cn(fact.due && 'text-status-wait')}>{fact.text}</span>
              </Fragment>
            ))}
          </ItemDescription>
        </ItemContent>
        <ItemActions>
          <ChevronRightIcon aria-hidden="true" className="size-4 text-subtle" />
        </ItemActions>
      </Link>
    </Item>
  )
}

// Results is a search through every project's wiki: the hits under the
// project whose they are, the project with the best hit first.
function Results({ query }: { query: string }) {
  const t = useT()
  const hits = useWikisSearch(query)
  const groups = useMemo(() => {
    const byProject = new Map<string, { name: string; hits: WikiProjectHit[] }>()
    for (const hit of hits.data ?? []) {
      const group = byProject.get(hit.project_id) ?? { name: hit.project_name, hits: [] }
      group.hits.push(hit)
      byProject.set(hit.project_id, group)
    }
    return [...byProject.entries()]
  }, [hits.data])
  if (hits.isPending) return <ListSkeleton />
  if (groups.length === 0) {
    return (
      <Empty className="px-4">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <SearchXIcon />
          </EmptyMedia>
          <EmptyTitle>{t('wiki.noMatch')}</EmptyTitle>
        </EmptyHeader>
      </Empty>
    )
  }
  return groups.map(([projectId, group]) => (
    <SidebarGroup key={projectId} className="py-1">
      <SidebarGroupLabel>
        <span className="truncate">{group.name}</span>
        <span className="ml-1.5 text-subtle tabular-nums">{group.hits.length}</span>
      </SidebarGroupLabel>
      <SidebarGroupContent>
        <ul aria-label={group.name} className="px-0">
          {group.hits.map((hit) => (
            <li key={hit.path}>
              <HitLink hit={hit} href={wikisHref(projectId, hit.path)} query={query} />
            </li>
          ))}
        </ul>
      </SidebarGroupContent>
    </SidebarGroup>
  ))
}
