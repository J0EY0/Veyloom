import { useDeferredValue, useMemo, useState } from 'react'
import { BrainIcon, ChevronRightIcon, HistoryIcon, LayoutGridIcon, NetworkIcon, PinIcon, ScrollTextIcon, SearchXIcon } from 'lucide-react'
import { Link } from 'react-router'
import type { WikiCatalog, WikiHit, WikiPageInfo } from '@/api/types'
import { useWikiSearch, type WikiSpace } from '@/api/wiki'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
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
import { Skeleton } from '@/components/ui/skeleton'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { changesHref, conventionsPage, graphHref, memoryHref, memoryPage, overviewHref, pageHref, type WikiRoute } from './links'
import { typeGroup, typeName } from './names'
import { problemText } from '@/api/errorText'

export interface WikiSidebarProps {
  space: WikiSpace
  catalog?: WikiCatalog
  loading: boolean
  route: WikiRoute
  className?: string
}

// A project wiki's list column, the way the inbox lists its entries: a
// search box, the overview, the memory, the wiki's own conventions when it
// has them, and the changes, then every page under its type, in the order
// the wiki keeps types in. Deprecated pages fold away
// at the end, and after them
// each bundle the project mounts, read-only (5.9). Typing searches the
// pages' text instead, the mounts' too (5.4).
export function WikiSidebar({ space, catalog, loading, route, className }: WikiSidebarProps) {
  const t = useT()
  const [query, setQuery] = useState('')
  const searching = useDeferredValue(query.trim())
  const groups = useMemo(() => groupPages(catalog), [catalog])
  const current = route.kind === 'page' ? route.path : ''

  return (
    <Sidebar collapsible="none" aria-label={t('wiki.pages')} className={cn('w-full md:w-68 md:flex-none md:border-r', className)}>
      <SidebarHeader className="px-3 pt-3 pb-1">
        <SidebarInput
          type="search"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder={t('wiki.search')}
          aria-label={t('wiki.search')}
          className="[&::-webkit-search-cancel-button]:appearance-none"
        />
      </SidebarHeader>
      <SidebarContent>
        {searching !== '' ? (
          <SearchResults space={space} query={searching} current={current} />
        ) : (
          <>
            <SidebarGroup className="pb-0">
              <SidebarMenu>
                <SidebarMenuItem>
                  <SidebarMenuButton asChild isActive={route.kind === 'overview'}>
                    <Link to={overviewHref(space)}>
                      <LayoutGridIcon />
                      <span>{t('wiki.overview')}</span>
                    </Link>
                  </SidebarMenuButton>
                </SidebarMenuItem>
                {space.kind === 'project' ? (
                  <SidebarMenuItem>
                    <SidebarMenuButton asChild isActive={route.kind === 'memory'}>
                      <Link to={memoryHref(space)}>
                        <BrainIcon />
                        <span>{t('wiki.memory')}</span>
                      </Link>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ) : null}
                {groups.conventions ? (
                  <SidebarMenuItem>
                    <SidebarMenuButton asChild isActive={current === conventionsPage}>
                      <Link to={pageHref(space, conventionsPage)}>
                        <ScrollTextIcon />
                        <span>{t('wiki.conventions')}</span>
                      </Link>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ) : null}
                <SidebarMenuItem>
                  <SidebarMenuButton asChild isActive={route.kind === 'changes'}>
                    <Link to={changesHref(space)}>
                      <HistoryIcon />
                      <span>{t('wiki.changes')}</span>
                    </Link>
                  </SidebarMenuButton>
                </SidebarMenuItem>
                {space.kind === 'project' ? (
                  <SidebarMenuItem>
                    <SidebarMenuButton asChild isActive={route.kind === 'graph'}>
                      <Link to={graphHref(space)}>
                        <NetworkIcon />
                        <span>{t('wiki.graph')}</span>
                      </Link>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ) : null}
              </SidebarMenu>
            </SidebarGroup>
            {loading ? <ListSkeleton /> : null}
            {groups.live.map((group) => (
              <SidebarGroup key={group.key} className="py-1">
                <SidebarGroupLabel>
                  <span className="truncate">{typeGroup(t, group.type)}</span>
                  <span className="ml-1.5 text-subtle tabular-nums">{group.pages.length}</span>
                </SidebarGroupLabel>
                <SidebarGroupContent>
                  <PageMenu pages={group.pages} space={space} current={current} />
                </SidebarGroupContent>
              </SidebarGroup>
            ))}
            {groups.deprecated.length > 0 ? (
              <Collapsible defaultOpen={groups.deprecated.some((page) => page.path === current)} className="group/deprecated">
                <SidebarGroup className="py-1">
                  <SidebarGroupLabel asChild>
                    <CollapsibleTrigger>
                      {t('wiki.deprecatedGroup')}
                      <span className="ml-1.5 text-subtle tabular-nums">{groups.deprecated.length}</span>
                      <ChevronRightIcon className="ml-auto transition-transform group-data-[state=open]/deprecated:rotate-90" />
                    </CollapsibleTrigger>
                  </SidebarGroupLabel>
                  <CollapsibleContent>
                    <PageMenu pages={groups.deprecated} space={space} current={current} dim />
                  </CollapsibleContent>
                </SidebarGroup>
              </Collapsible>
            ) : null}
            {(catalog?.mounts ?? []).map((mount) => (
              <Collapsible key={mount.name} defaultOpen={mount.pages.some((page) => page.path === current)} className="group/mount">
                <SidebarGroup className="py-1">
                  <SidebarGroupLabel asChild>
                    <CollapsibleTrigger>
                      <span className="truncate">{t('wiki.mount', { name: mount.name })}</span>
                      <span className="ml-1.5 text-subtle tabular-nums">{mount.pages.length}</span>
                      <ChevronRightIcon className="ml-auto transition-transform group-data-[state=open]/mount:rotate-90" />
                    </CollapsibleTrigger>
                  </SidebarGroupLabel>
                  <CollapsibleContent>
                    {mount.error ? (
                      <p className="px-2 py-1 text-xs break-words text-status-fail">
                        {t('wiki.mountError', { error: problemText(mount.error_code, mount.error_params, mount.error) })}
                      </p>
                    ) : (
                      <PageMenu pages={[...mount.pages].sort((a, b) => a.title.localeCompare(b.title))} space={space} current={current} />
                    )}
                  </CollapsibleContent>
                </SidebarGroup>
              </Collapsible>
            ))}
          </>
        )}
      </SidebarContent>
    </Sidebar>
  )
}

function PageMenu({ pages, space, current, dim }: { pages: WikiPageInfo[]; space: WikiSpace; current: string; dim?: boolean }) {
  const t = useT()
  return (
    <SidebarMenu>
      {pages.map((page) => (
        <SidebarMenuItem key={page.path}>
          <SidebarMenuButton asChild size="sm" isActive={page.path === current} className={cn(dim && 'text-muted-foreground')}>
            <Link to={pageHref(space, page.path)}>
              <span className="truncate">{page.title}</span>
              {page.resident ? (
                <>
                  <PinIcon className="ml-auto text-subtle" aria-hidden="true" />
                  <span className="sr-only">{t('wiki.resident')}</span>
                </>
              ) : null}
            </Link>
          </SidebarMenuButton>
        </SidebarMenuItem>
      ))}
    </SidebarMenu>
  )
}

function SearchResults({ space, query, current }: { space: WikiSpace; query: string; current: string }) {
  const t = useT()
  const hits = useWikiSearch(space, query)
  if (hits.isPending) return <ListSkeleton />
  if (!hits.data || hits.data.length === 0) {
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
  return (
    <ul className="px-2 py-1">
      {hits.data.map((hit) => (
        <li key={hit.path}>
          <HitLink hit={hit} href={pageHref(space, hit.path)} current={hit.path === current} />
        </li>
      ))}
    </ul>
  )
}

// HitLink is a page a search found: its title, what it is and where, and
// the words around the match.
export function HitLink({ hit, href, current = false }: { hit: WikiHit; href: string; current?: boolean }) {
  const t = useT()
  return (
    <Link
      to={href}
      aria-current={current ? 'page' : undefined}
      className="flex flex-col gap-1 rounded-md px-2 py-2 text-sm outline-hidden hover:bg-sidebar-accent/70 focus-visible:ring-2 focus-visible:ring-sidebar-ring aria-[current=page]:bg-sidebar-accent"
    >
      <span className={cn('truncate font-medium', hit.status === 'deprecated' && 'text-muted-foreground line-through')}>{hit.title}</span>
      <span className="truncate text-xs text-subtle">
        {hit.mount ? `${t('wiki.mount', { name: hit.mount })} · ` : null}
        {typeName(t, hit.type)} · <span translate="no">{hit.path}</span>
      </span>
      {hit.snippet ? <span className="line-clamp-2 text-xs leading-normal text-muted-foreground">{hit.snippet}</span> : null}
    </Link>
  )
}

export function ListSkeleton() {
  const t = useT()
  return (
    <div role="status" aria-label={t('common.loading')} className="flex flex-col gap-2.5 px-4 py-3">
      {[0, 1, 2, 3].map((row) => (
        <Skeleton key={row} className="h-3.5 w-full" />
      ))}
    </div>
  )
}

// PageGroup is one heading of the list: a type of page.
export interface PageGroup {
  key: string
  type: string
  pages: WikiPageInfo[]
}

// groupPages files the pages under their types, in the wiki's order and
// then any other type by name; each type's pages by title. The project
// memory has a place of its own above, and so have the wiki's conventions,
// when it has them.
export function groupPages(catalog?: WikiCatalog): { live: PageGroup[]; deprecated: WikiPageInfo[]; conventions: boolean } {
  if (!catalog) return { live: [], deprecated: [], conventions: false }
  const byTitle = (a: WikiPageInfo, b: WikiPageInfo) => a.title.localeCompare(b.title)
  const byType = new Map<string, WikiPageInfo[]>()
  let conventions = false
  for (const page of catalog.pages) {
    if (page.path === conventionsPage && page.status !== 'deprecated' && !page.mount) {
      conventions = true
      continue
    }
    if (page.status === 'deprecated' || page.path === memoryPage) continue
    byType.set(page.type, [...(byType.get(page.type) ?? []), page])
  }
  const order = catalog.dirs.map((dir) => dir.type)
  const types = [...order.filter((type) => byType.has(type)), ...[...byType.keys()].filter((type) => !order.includes(type)).sort()]
  return {
    live: types.map((type) => ({ key: type, type, pages: (byType.get(type) ?? []).sort(byTitle) })),
    deprecated: catalog.pages.filter((page) => page.status === 'deprecated').sort(byTitle),
    conventions,
  }
}
