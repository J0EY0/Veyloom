import { useCallback, useState } from 'react'
import { BookXIcon, EllipsisIcon, FolderInputIcon } from 'lucide-react'
import { Link, Navigate, useNavigate, useParams } from 'react-router'
import { toast } from 'sonner'
import { errorText } from '@/api/errorText'
import { librarySpace, useWikiCatalog } from '@/api/wiki'
import { HeaderTabs } from '@/components/layout/HeaderTabs'
import { Panel } from '@/components/layout/Panel'
import { PanelHeader } from '@/components/layout/PanelHeader'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { copyText } from '@/lib/clipboard'
import { useDocumentTitle } from '@/lib/useDocumentTitle'
import { useT } from '@/lib/i18n'
import type { OpenTopic } from './CommitRow'
import { ImportSkillDialog } from './ImportSkillDialog'
import { changesHref, pageHref, topicHref } from './links'
import { WikiChanges } from './WikiOverview'
import { LibraryDetail } from './library/LibraryDetail'
import { PatternList } from './library/PatternList'
import { SkillList } from './library/SkillList'
import { libraryRoute } from './library/model'

const patternsHref = pageHref(librarySpace, '/patterns')

// The skill library every project shares (docs/design.md 5.10, 5.15), laid
// out as a market of skills (docs/webui.md 4.10): the skills to find and
// install for agents, and on a tab of their own the patterns they rest on;
// each opens on a page of its own. People import skills from a folder;
// the changes and the folder are in the "…" menu.
export function LibraryPage() {
  const t = useT()
  useDocumentTitle(t('library.title'))
  const navigate = useNavigate()
  const route = libraryRoute(useParams()['*'] ?? '')
  const catalog = useWikiCatalog(librarySpace)
  const [importing, setImporting] = useState(false)
  // The library is no chat's: a topic opens in the chat it is of.
  const openTopic: OpenTopic = useCallback(
    (threadId, roomId) => {
      if (roomId) void navigate(topicHref(roomId, threadId))
    },
    [navigate],
  )

  // The front page and the graph of the library's old layout.
  if (route.kind === 'old') return <Navigate to={pageHref(librarySpace)} replace />
  const onPatterns = route.kind === 'patterns' || (route.kind === 'page' && route.path.startsWith('/patterns/'))
  return (
    <Panel>
      <PanelHeader
        title={t('library.title')}
        actions={
          <HeaderTabs
            label={t('library.views')}
            tabs={[
              { to: pageHref(librarySpace), active: !onPatterns && route.kind !== 'changes', label: t('library.tab.skills') },
              { to: patternsHref, active: onPatterns, label: t('library.tab.patterns') },
            ]}
          />
        }
        trailing={
          <>
            {/* On a phone the bar has no room for it: it is in the menu. */}
            <Button variant="outline" size="sm" onClick={() => setImporting(true)} className="hidden sm:inline-flex">
              <FolderInputIcon />
              {t('library.import.open')}
            </Button>
            <LibraryMenu folder={catalog.data?.folder} onImport={() => setImporting(true)} />
          </>
        }
      />
      {catalog.isError ? (
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <BookXIcon />
            </EmptyMedia>
            <EmptyTitle>{t('wiki.failed')}</EmptyTitle>
            <EmptyDescription>{errorText(catalog.error)}</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : route.kind === 'skills' ? (
        <SkillList catalog={catalog.data} onImport={() => setImporting(true)} />
      ) : route.kind === 'patterns' ? (
        <PatternList catalog={catalog.data} />
      ) : route.kind === 'changes' ? (
        <div className="min-h-0 flex-1 overflow-y-auto">
          {catalog.data ? (
            <WikiChanges
              space={librarySpace}
              catalog={catalog.data}
              onOpenThread={openTopic}
              back={{ href: pageHref(librarySpace), label: t('library.title') }}
            />
          ) : null}
        </div>
      ) : (
        <LibraryDetail path={route.path} catalog={catalog.data} onOpenThread={openTopic} />
      )}
      {importing ? <ImportSkillDialog onClose={() => setImporting(false)} /> : null}
    </Panel>
  )
}

// LibraryMenu holds what the library has besides its lists: the record of
// its changes, and where it is on disk; on a phone, importing too.
function LibraryMenu({ folder, onImport }: { folder?: string; onImport: () => void }) {
  const t = useT()
  async function copyFolder(path: string) {
    if (await copyText(path)) toast.success(t('wiki.pathCopied'))
    else toast.error(t('common.copyFailed'))
  }
  return (
    <DropdownMenu>
      <Tooltip>
        <TooltipTrigger asChild>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon-sm" aria-label={t('common.more')} className="text-subtle hover:text-foreground">
              <EllipsisIcon />
            </Button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent side="bottom">{t('common.more')}</TooltipContent>
      </Tooltip>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onSelect={onImport} className="sm:hidden">
          {t('library.import.open')}
        </DropdownMenuItem>
        <DropdownMenuItem asChild>
          <Link to={changesHref(librarySpace)}>{t('library.changes')}</Link>
        </DropdownMenuItem>
        {folder ? <DropdownMenuItem onSelect={() => void copyFolder(folder)}>{t('wiki.copyFolder')}</DropdownMenuItem> : null}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
