import { useState } from 'react'
import { BookOpenIcon, ChevronLeftIcon, CopyIcon } from 'lucide-react'
import { Link } from 'react-router'
import { toast } from 'sonner'
import type { WikiCatalog, WikiMount } from '@/api/types'
import { useWikiHistory, type WikiSpace } from '@/api/wiki'
import { Button } from '@/components/ui/button'
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Spinner } from '@/components/ui/spinner'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { copyText } from '@/lib/clipboard'
import { useT } from '@/lib/i18n'
import { CommitRow, type OpenTopic } from './CommitRow'
import { MaintainerCard } from './Maintainer'
import { changesHref, memoryPage, pageHref } from './links'
import { typeName } from './names'
import { reviewOrder, reviewText } from './review'
import { problemText } from '@/api/errorText'

export interface WikiOverviewProps {
  space: WikiSpace
  catalog: WikiCatalog
  onOpenThread: OpenTopic
}

// The wiki's front page: how many pages and where they are on disk, the
// pages every turn carries, and the latest changes.
export function WikiOverview({ space, catalog, onOpenThread }: WikiOverviewProps) {
  const t = useT()
  const recent = useWikiHistory(space, '', 5)
  // The project memory is not one of the pages: it has a place of its own.
  const pages = catalog.pages.filter((page) => page.path !== memoryPage)
  const live = pages.filter((page) => page.status !== 'deprecated')
  const resident = pages.filter((page) => page.resident)
  const due = reviewOrder(live)

  async function copyFolder() {
    if (await copyText(catalog.folder)) toast.success(t('wiki.pathCopied'))
    else toast.error(t('common.copyFailed'))
  }

  return (
    // At least as tall as the pane, so the note of an empty wiki takes the
    // rest of it and sits in the middle.
    <div className="mx-auto flex min-h-full w-full max-w-[46rem] flex-col gap-8 px-5 pt-5 pb-12 @split/panel:px-10">
      <header className="flex flex-col gap-1">
        <BackToPages space={space} />
        <h1 className="text-xl font-semibold tracking-[-0.01em]">{t('wiki.title')}</h1>
        <div className="flex min-w-0 items-center gap-1.5 text-xs text-subtle">
          <span className="flex-none">{t('wiki.pageCount', { n: live.length })}</span>
          {due.length > 0 ? <span className="flex-none text-status-wait">· {t('wiki.reviewCount', { n: due.length })}</span> : null}
          <span aria-hidden="true">·</span>
          <span className="truncate font-mono" translate="no">
            {catalog.folder}
          </span>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="ghost" size="icon-xs" aria-label={t('wiki.copyFolder')} onClick={() => void copyFolder()} className="flex-none text-subtle">
                <CopyIcon />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">{t('wiki.copyFolder')}</TooltipContent>
          </Tooltip>
        </div>
      </header>

      {space.kind === 'project' ? <MaintainerCard projectId={space.projectId} roomId={space.roomId} onOpenThread={onOpenThread} /> : null}

      {catalog.mounts && catalog.mounts.length > 0 ? <Mounts mounts={catalog.mounts} /> : null}

      {pages.length === 0 ? (
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <BookOpenIcon />
            </EmptyMedia>
            <EmptyTitle>{t('wiki.empty')}</EmptyTitle>
          </EmptyHeader>
        </Empty>
      ) : null}

      {resident.length > 0 ? (
        <section aria-labelledby="wiki-resident">
          <h2 id="wiki-resident" className="text-sm font-semibold">
            {t('wiki.residentPages')}
          </h2>
          <ul className="mt-2 grid gap-1 text-[0.875rem]">
            {resident.map((page) => (
              <li key={page.path} className="flex min-w-0 items-baseline gap-2">
                <Link to={pageHref(space, page.path)} className="truncate underline-offset-3 hover:underline">
                  {page.title}
                </Link>
                <span className="flex-none text-xs text-subtle">{typeName(t, page.type)}</span>
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      {due.length > 0 ? (
        <section aria-labelledby="wiki-review">
          <h2 id="wiki-review" className="text-sm font-semibold">
            {t('wiki.reviewPages')}
          </h2>
          <ul className="mt-2 grid gap-2 text-[0.875rem]">
            {due.map((page) => (
              <li key={page.path} className="flex min-w-0 flex-col gap-0.5">
                <Link to={pageHref(space, page.path)} className="truncate underline-offset-3 hover:underline">
                  {page.title}
                </Link>
                <span className="text-xs break-words text-subtle">{reviewText(t, page)}</span>
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      {pages.length > 0 ? (
        <section aria-labelledby="wiki-recent">
          <div className="flex items-baseline gap-2">
            <h2 id="wiki-recent" className="text-sm font-semibold">
              {t('wiki.recent')}
            </h2>
            <span className="grow" />
            <Link to={changesHref(space)} className="text-xs text-muted-foreground underline-offset-3 hover:text-foreground hover:underline">
              {t('wiki.allChanges')}
            </Link>
          </div>
          {recent.isPending ? (
            <p role="status" className="flex items-center gap-2 py-2 text-xs text-subtle">
              <Spinner className="size-3" />
              {t('common.loading')}
            </p>
          ) : (
            // What changed, at a glance: undoing is the changes page's, a
            // click on 全部变更 away.
            <ul className="divide-y">
              {(recent.data ?? []).map((commit) => (
                <CommitRow key={commit.sha} commit={commit} space={space} canUndo={false} teams={catalog.teams} onOpenThread={onOpenThread} />
              ))}
            </ul>
          )}
        </section>
      ) : null}
    </div>
  )
}

// The whole history, newest first, a page of changes at a time. Shown alone,
// it leads back where back says.
export function WikiChanges({ space, catalog, onOpenThread, back }: WikiOverviewProps & { back?: Back }) {
  const t = useT()
  const [limit, setLimit] = useState(50)
  const commits = useWikiHistory(space, '', limit)
  return (
    <div className="mx-auto flex min-h-full w-full max-w-[46rem] flex-col gap-8 px-5 pt-5 pb-12 @split/panel:px-10">
      <header className="flex flex-col gap-1">
        {back ? <BackLink back={back} /> : <BackToPages space={space} />}
        <h1 className="text-xl font-semibold tracking-[-0.01em]">{t('wiki.changes')}</h1>
      </header>
      {commits.data?.length === 0 ? (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>{t('wiki.changes.empty')}</EmptyTitle>
          </EmptyHeader>
        </Empty>
      ) : (
        <section aria-label={t('wiki.changes')}>
          {commits.isPending ? (
            <p role="status" className="flex items-center gap-2 py-2 text-xs text-subtle">
              <Spinner className="size-3" />
              {t('common.loading')}
            </p>
          ) : commits.isError ? (
            <p role="alert" className="text-sm text-status-fail">
              {t('wiki.changes.failed')}
            </p>
          ) : (
            <ul className="divide-y">
              {commits.data.map((commit) => (
                <CommitRow key={commit.sha} commit={commit} space={space} canUndo={catalog.history} teams={catalog.teams} onOpenThread={onOpenThread} />
              ))}
            </ul>
          )}
          {commits.data && commits.data.length >= limit ? (
            <Button
              variant="ghost"
              size="sm"
              className="mt-2 w-full text-muted-foreground"
              onClick={() => setLimit((n) => n + 50)}
              disabled={commits.isFetching}
            >
              {commits.isFetching ? t('common.loading') : t('wiki.changes.more')}
            </Button>
          ) : null}
        </section>
      )}
    </div>
  )
}

// Back is a way back from a page shown alone, as the library's are: where
// to, and what it is called.
export interface Back {
  href: string
  label: string
}

// BackLink leads back from a page that has the whole width, on any screen.
export function BackLink({ back }: { back: Back }) {
  return (
    <Link to={back.href} className="-ml-1 flex w-fit min-w-0 items-center gap-0.5 text-xs text-subtle hover:text-foreground">
      <ChevronLeftIcon className="size-4 flex-none" aria-hidden="true" />
      <span className="truncate">{back.label}</span>
    </Link>
  )
}

// BackToPages leads a narrow screen, which shows one pane at a time, back
// to the list of pages.
export function BackToPages({ space }: { space: WikiSpace }) {
  const t = useT()
  return (
    <Link to={pageHref(space)} className="-ml-1 flex w-fit items-center gap-0.5 text-xs text-subtle hover:text-foreground @split/panel:hidden">
      <ChevronLeftIcon className="size-4" aria-hidden="true" />
      {t('wiki.pages')}
    </Link>
  )
}

// Mounts lists the bundles the project mounts: their names, how many
// pages, and where they are on disk.
function Mounts({ mounts }: { mounts: WikiMount[] }) {
  const t = useT()
  return (
    <section aria-labelledby="wiki-mounts">
      <h2 id="wiki-mounts" className="text-sm font-semibold">
        {t('wiki.mounts')}
      </h2>
      <ul className="mt-2 grid gap-1 text-[0.875rem]">
        {mounts.map((mount) => (
          <li key={mount.name} className="flex min-w-0 items-baseline gap-2">
            <span className="flex-none font-medium">{mount.name}</span>
            <span className={mount.error ? 'min-w-0 truncate text-xs text-status-fail' : 'flex-none text-xs text-subtle'}>
              {mount.error
                ? t('wiki.mountError', { error: problemText(mount.error_code, mount.error_params, mount.error) })
                : t('wiki.mountPages', { n: mount.pages.length })}
            </span>
            {mount.error ? null : (
              <span className="min-w-0 truncate font-mono text-xs text-subtle" translate="no">
                {mount.folder}
              </span>
            )}
          </li>
        ))}
      </ul>
    </section>
  )
}
