import { useCallback } from 'react'
import { BookXIcon } from 'lucide-react'
import { Navigate, useNavigate } from 'react-router'
import { useWikiCatalog, type WikiSpace } from '@/api/wiki'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import type { OpenTopic } from './CommitRow'
import { pageHref, topicHref, wikiRoute } from './links'
import { ProjectMemory } from './ProjectMemory'
import { WikiChanges, WikiOverview } from './WikiOverview'
import { WikiPageView } from './WikiPageView'
import { WikiSidebar } from './WikiSidebar'
import { WikiGraphView } from './graph/WikiGraphView'
import { errorText } from '@/api/errorText'

export interface WikiViewProps {
  space: WikiSpace
  // The part of the address after the wiki's own: a page's path, "changes",
  // "memory" (a project's), "graph", or nothing for the overview.
  rest: string
  // Opens a topic of the chat the wiki is shown in, beside it.
  onOpenThread?: (threadId: string) => void
}

// A wiki read in two columns (docs/design.md 5.14, step 4; webui.md 4.9):
// the pages on the left, the one open on the right, the way the inbox
// reads. A chat's Wiki tab shows the project's; the library page the skill
// library. In a narrow page one of the two at a time (see Panel): the list
// at the wiki's root, a page otherwise.
export function WikiView({ space, rest, onOpenThread }: WikiViewProps) {
  const t = useT()
  const navigate = useNavigate()
  const route = wikiRoute(rest)
  const catalog = useWikiCatalog(space)
  // A narrow screen shows the list at the wiki's root, and what it opens
  // anywhere else, the front page by name included.
  const atRoot = rest.replace(/^\/+|\/+$/g, '') === ''
  // A topic of the chat the wiki is in opens beside it; one elsewhere, as
  // the library's sources are, or any when the wiki is read standalone, in
  // its own chat. A topic naming no chat is of the project's.
  const openTopic: OpenTopic = useCallback(
    (threadId, roomId) => {
      const room = roomId ?? (space.kind === 'project' ? space.roomId : undefined)
      if (space.kind === 'project' && onOpenThread && room === space.roomId) onOpenThread(threadId)
      else if (room) void navigate(topicHref(room, threadId))
    },
    [space, onOpenThread, navigate],
  )

  // The graph is a project wiki's (docs/design.md 5.17); the skill
  // library's address for it leads to the library's front page.
  if (route.kind === 'graph' && space.kind !== 'project') return <Navigate to={pageHref(space)} replace />
  if (catalog.isError) {
    return (
      <Empty className="h-full">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <BookXIcon />
          </EmptyMedia>
          <EmptyTitle>{t('wiki.failed')}</EmptyTitle>
          <EmptyDescription>{errorText(catalog.error)}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }
  return (
    <div className="flex min-h-0 flex-1">
      <WikiSidebar space={space} catalog={catalog.data} loading={catalog.isPending} route={route} className={atRoot ? 'flex' : 'hidden @split/panel:flex'} />
      <div className={cn('min-w-0 flex-1', route.kind === 'graph' ? 'overflow-hidden' : 'overflow-y-auto', atRoot ? 'hidden @split/panel:block' : 'block')}>
        {route.kind === 'graph' && space.kind === 'project' ? (
          <WikiGraphView space={space} onOpenThread={openTopic} />
        ) : route.kind === 'page' ? (
          <WikiPageView
            key={route.path}
            space={space}
            path={route.path}
            history={catalog.data?.history ?? true}
            teams={catalog.data?.teams}
            onOpenThread={openTopic}
          />
        ) : !catalog.data ? (
          <div role="status" aria-label={t('common.loading')} className="mx-auto flex w-full max-w-[46rem] flex-col gap-3 px-5 pt-6 @split/panel:px-10">
            <Skeleton className="h-6 w-40" />
            <Skeleton className="h-3 w-64" />
          </div>
        ) : route.kind === 'changes' ? (
          <WikiChanges space={space} catalog={catalog.data} onOpenThread={openTopic} />
        ) : route.kind === 'memory' && space.kind === 'project' ? (
          <ProjectMemory space={space} onOpenThread={openTopic} />
        ) : (
          <WikiOverview space={space} catalog={catalog.data} onOpenThread={openTopic} />
        )}
      </div>
    </div>
  )
}
