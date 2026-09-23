import { Fragment, useMemo, useState } from 'react'
import { LightbulbIcon } from 'lucide-react'
import { Link } from 'react-router'
import type { WikiCatalog } from '@/api/types'
import { librarySpace } from '@/api/wiki'
import { PageBody } from '@/components/layout/PageBody'
import { Button } from '@/components/ui/button'
import { Empty, EmptyContent, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Item, ItemContent, ItemDescription, ItemGroup, ItemSeparator, ItemTitle } from '@/components/ui/item'
import { formatTime } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { pageHref } from '../links'
import { actorName } from '../names'
import { SearchBox } from './SearchBox'
import { ListSkeleton } from './SkillList'
import { filterPages, patternPages } from './model'

// PatternList is the library's other tab (docs/webui.md 4.10): what
// members found about how tasks go wrong or right, which skills rest on.
// A row each: its title, what it is about, and who wrote it when.
export function PatternList({ catalog }: { catalog?: WikiCatalog }) {
  const t = useT()
  const [query, setQuery] = useState('')
  const pages = useMemo(() => patternPages(catalog), [catalog])

  if (!catalog) return <ListSkeleton />
  if (pages.length === 0) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <LightbulbIcon />
          </EmptyMedia>
          <EmptyTitle>{t('library.patternsEmpty')}</EmptyTitle>
        </EmptyHeader>
      </Empty>
    )
  }
  const shown = filterPages(pages, query)
  return (
    <PageBody width="narrow" fill={shown.length === 0} className="pt-2">
      <div className="flex flex-wrap items-center gap-2">
        <SearchBox value={query} onChange={setQuery} label={t('library.searchPatterns')} />
      </div>
      {shown.length === 0 ? (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>{t('library.patternsNoMatch')}</EmptyTitle>
          </EmptyHeader>
          <EmptyContent>
            <Button variant="outline" size="sm" onClick={() => setQuery('')}>
              {t('agents.clearFilters')}
            </Button>
          </EmptyContent>
        </Empty>
      ) : (
        <ItemGroup aria-label={t('library.tab.patterns')} className="-mx-3">
          {shown.map((page, index) => (
            <Fragment key={page.path}>
              {index > 0 ? <ItemSeparator className="mx-3 data-[orientation=horizontal]:w-auto" /> : null}
              {/* The row is a link; the list item holds it, as a link
                  cannot be one. */}
              <div role="listitem">
                <Item asChild className="rounded-lg px-3 py-3">
                  <Link to={pageHref(librarySpace, page.path)}>
                    <ItemContent className="min-w-0 gap-1">
                      <ItemTitle className={page.status === 'deprecated' ? 'text-muted-foreground line-through' : undefined}>{page.title}</ItemTitle>
                      {page.description ? <ItemDescription className="text-[0.8125rem] text-pretty">{page.description}</ItemDescription> : null}
                      {page.generated_by && page.generated_at ? (
                        <p className="text-xs text-subtle">
                          {t('wiki.page.writtenBy', { who: actorName(page.generated_by), when: formatTime(page.generated_at) })}
                        </p>
                      ) : null}
                    </ItemContent>
                  </Link>
                </Item>
              </div>
            </Fragment>
          ))}
        </ItemGroup>
      )}
    </PageBody>
  )
}
