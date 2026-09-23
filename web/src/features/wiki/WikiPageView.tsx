import type { ReactNode } from 'react'
import { ChevronLeftIcon, EllipsisIcon, FileQuestionIcon, PinIcon } from 'lucide-react'
import { Link } from 'react-router'
import { toast } from 'sonner'
import { ApiError } from '@/api/client'
import type { WikiPage, WikiTeam } from '@/api/types'
import { skillName, useChangeWikiPage, useWikiPage, type WikiSpace } from '@/api/wiki'
import { StatusPill } from '@/components/shared/status-pill'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { copyText } from '@/lib/clipboard'
import { formatTime } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { runtimeName } from '@/lib/runtimes'
import type { OpenTopic } from './CommitRow'
import { withoutTitleHeading } from './body'
import { pageHref } from './links'
import { actorName, tierName, typeName, vouched } from './names'
import { PageHistory, PageSources } from './PageParts'
import { PageRelations } from './PageRelations'
import { QuestionButton } from './QuestionButton'
import { reviewText } from './review'
import { SkillInstalls, SkillTeam, SkillUses } from './SkillParts'
import { SkillTrialPart } from './SkillTrial'
import { keptFor } from './skills'
import { BackLink, type Back } from './WikiOverview'
import { WikiMarkdown } from './WikiMarkdown'
import { errorText } from '@/api/errorText'

export interface WikiPageViewProps {
  space: WikiSpace
  path: string
  // The wiki keeps a history to undo from.
  history: boolean
  // The teams owning the library's skills.
  teams?: WikiTeam[]
  onOpenThread: OpenTopic
  // Shown alone, as the library's pages are: the way back, in place of the
  // kind and path of the page.
  back?: Back
  // What follows the text, like the files a skill came with.
  afterBody?: ReactNode
}

// One page of a wiki, read (docs/design.md 5.3): what kind it is and how far
// to trust it, who wrote it and who confirmed it, its text, where it came
// from and what changed it. A person confirms it here, or in a project's
// wiki makes it one every turn carries. A skill of the library also says which team owns it, whom it is
// installed for, how its trial goes, and which turns used it.
export function WikiPageView({ space, path, history, teams = [], onOpenThread, back, afterBody }: WikiPageViewProps) {
  const t = useT()
  const page = useWikiPage(space, path)

  if (page.isPending) return <PageSkeleton />
  if (page.isError) {
    const missing = page.error instanceof ApiError && page.error.status === 404
    return (
      <Empty className="h-full">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <FileQuestionIcon />
          </EmptyMedia>
          <EmptyTitle>
            {!missing ? t('wiki.page.failed') : space.kind === 'library' && skillName(path) ? t('library.skillMissing') : t('wiki.page.missing')}
          </EmptyTitle>
          {missing ? null : <EmptyDescription>{errorText(page.error)}</EmptyDescription>}
        </EmptyHeader>
        <EmptyContent>
          <Button variant="outline" size="sm" asChild>
            <Link to={back?.href ?? pageHref(space)}>{back?.label ?? t('wiki.overview')}</Link>
          </Button>
        </EmptyContent>
      </Empty>
    )
  }
  const data = page.data
  const skill = space.kind === 'library' && data.type === 'Skill'
  // A page of a bundle the project mounts is read, never changed here.
  const mounted = Boolean(data.mount)
  return (
    <article className="mx-auto w-full max-w-[46rem] px-5 pt-5 pb-12 md:px-10">
      <PageHead page={data} space={space} onOpenThread={onOpenThread} back={back} />
      {skill ? (
        <div className="mt-2 flex flex-col gap-1.5">
          <SkillTeam page={data} teams={teams} />
          <SkillInstalls page={data} />
        </div>
      ) : null}
      {skill ? <SkillTrialPart page={data} /> : null}
      <WikiMarkdown text={withoutTitleHeading(data.body, data.title)} from={data.path} space={space} className="mt-6 text-[0.9375rem] leading-[1.7]" />
      {afterBody}
      <PageSources page={data} space={space} onOpenThread={onOpenThread} />
      <PageRelations page={data} space={space} onOpenThread={onOpenThread} />
      {skill ? <SkillUses name={data.path.split('/')[2] ?? ''} /> : null}
      {mounted ? null : <PageHistory page={data} space={space} canUndo={history} teams={teams} onOpenThread={onOpenThread} />}
    </article>
  )
}

function PageHead({ page, space, onOpenThread, back }: { page: WikiPage; space: WikiSpace; onOpenThread: OpenTopic; back?: Back }) {
  const t = useT()
  const change = useChangeWikiPage(space)
  const confirmed = vouched(page)
  // Resident pages are a project wiki's; the library's go to runtimes as skills.
  const tagged = space.kind === 'project' && page.tags.some((tag) => tag.toLowerCase() === 'resident')
  const lastPerson = [...page.verified].reverse().find((stamp) => stamp.by.startsWith('human:'))
  // The maintainer's confirmation, when it is the latest (confirm_wiki,
  // docs/design.md 5.16).
  const latest = page.verified.at(-1)
  const lastMachine = latest && !latest.by.startsWith('human:') ? latest : undefined
  const failed = (err: Error) => toast.error(t('wiki.page.changeFailed', { error: errorText(err) }))
  // Tags keeping a skill for some runtimes read as whom it is for.
  const kept = keptFor(page)

  async function copy(text: string, done: string) {
    if (await copyText(text)) toast.success(done)
    else toast.error(t('common.copyFailed'))
  }

  return (
    <header className="flex flex-col gap-2">
      <div className="flex min-w-0 items-center gap-2 text-xs text-subtle">
        {back ? (
          <BackLink back={back} />
        ) : (
          <>
            <Link to={pageHref(space)} className="-ml-1 flex flex-none items-center rounded hover:text-foreground md:hidden" aria-label={t('wiki.overview')}>
              <ChevronLeftIcon className="size-4" />
            </Link>
            <span className="flex-none">{typeName(t, page.type)}</span>
            <span className="truncate font-mono" translate="no">
              {page.path}
            </span>
          </>
        )}
        <span className="grow" />
        {space.kind === 'project' && !page.mount ? <QuestionButton page={page} projectId={space.projectId} onOpenThread={onOpenThread} /> : null}
        {/* A skill on trial is kept by the trial's own button. */}
        {!confirmed && !page.mount && page.trial?.status !== 'open' ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="outline"
                size="xs"
                disabled={change.isPending}
                onClick={() => change.mutate({ path: page.path, verify: true }, { onError: failed })}
              >
                {change.isPending ? t('wiki.page.confirming') : t('wiki.page.confirm')}
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom" className="max-w-60">
              {t('wiki.page.confirmHint')}
            </TooltipContent>
          </Tooltip>
        ) : null}
        {space.kind === 'project' && !page.mount && (page.resident || !tagged) ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="xs"
                disabled={change.isPending}
                onClick={() => change.mutate({ path: page.path, resident: !page.resident }, { onError: failed })}
                className="text-muted-foreground"
              >
                <PinIcon />
                {page.resident ? t('wiki.page.stopResident') : t('wiki.page.makeResident')}
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom" className="max-w-60">
              {t('wiki.page.residentHint')}
            </TooltipContent>
          </Tooltip>
        ) : null}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon-xs" aria-label={t('common.more')} className="text-subtle">
              <EllipsisIcon />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onSelect={() => void copy(page.file, t('wiki.pathCopied'))}>{t('wiki.page.copyFile')}</DropdownMenuItem>
            <DropdownMenuItem onSelect={() => void copy(location.href, t('common.linkCopied'))}>{t('common.copyLink')}</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <h1 className="text-xl font-semibold tracking-[-0.01em] break-words">{page.title}</h1>
      {page.description ? <p className="text-[0.9375rem] leading-relaxed text-muted-foreground">{page.description}</p> : null}
      <div className="flex flex-wrap items-center gap-1.5">
        {page.mount ? (
          <StatusPill tone="idle" dot={false}>
            {t('wiki.mount', { name: page.mount })} · {t('wiki.mountReadOnly')}
          </StatusPill>
        ) : null}
        <StatusPill tone={page.tier === 'human-reviewed' ? 'ok' : 'idle'} dot={false}>
          {tierName(t, page.tier)}
        </StatusPill>
        {page.resident ? (
          <StatusPill tone="idle" dot={false}>
            <PinIcon className="size-3" aria-hidden="true" />
            {t('wiki.resident')}
          </StatusPill>
        ) : null}
        {page.status === 'deprecated' ? <StatusPill tone="fail">{t('wiki.status.deprecated')}</StatusPill> : null}
        {page.status === 'draft' ? <StatusPill tone="idle">{t('wiki.status.draft')}</StatusPill> : null}
        {page.review ? <StatusPill tone="wait">{t('wiki.review.due')}</StatusPill> : page.stale ? <StatusPill tone="wait">{t('wiki.stale')}</StatusPill> : null}
        {kept.length > 0 ? (
          <StatusPill tone="idle" dot={false}>
            {t('library.onlyFor', { runtimes: kept.map(runtimeName).join('、') })}
          </StatusPill>
        ) : null}
        {page.tags
          .filter((tag) => tag.toLowerCase() !== 'resident' && !tag.toLowerCase().startsWith('runtime-'))
          .map((tag) => (
            <Badge key={tag} variant="outline" className="rounded-md px-1.5 text-[0.6875rem] font-normal text-muted-foreground">
              {tag}
            </Badge>
          ))}
      </div>
      <p className="text-xs text-subtle">
        {page.generated_by && page.generated_at ? t('wiki.page.writtenBy', { who: actorName(page.generated_by), when: formatTime(page.generated_at) }) : null}
        {page.generated_by && page.generated_at ? ' · ' : null}
        {lastPerson ? t('wiki.page.confirmedBy', { who: actorName(lastPerson.by), when: formatTime(lastPerson.at) }) : t('wiki.page.notConfirmed')}
        {lastMachine ? ` · ${t('wiki.page.machineChecked', { who: actorName(lastMachine.by), when: formatTime(lastMachine.at) })}` : null}
      </p>
      {page.review ? (
        <p className="text-xs leading-relaxed text-status-wait">
          {t('wiki.review.due')}：{reviewText(t, page)}
          {page.review.why === 'changed' && page.review.thread_id && page.review.topic_number ? (
            <>
              {' · '}
              <button
                type="button"
                onClick={() => onOpenThread(page.review?.thread_id as string, page.review?.room_id)}
                className="underline-offset-3 hover:underline"
              >
                {t('wiki.review.topic', { n: page.review.topic_number })}
              </button>
            </>
          ) : null}
        </p>
      ) : null}
    </header>
  )
}

function PageSkeleton() {
  const t = useT()
  return (
    <div role="status" aria-label={t('common.loading')} className="mx-auto flex w-full max-w-[46rem] flex-col gap-3 px-5 pt-6 md:px-10">
      <Skeleton className="h-3 w-40" />
      <Skeleton className="h-6 w-2/3" />
      <Skeleton className="h-4 w-full" />
      <Skeleton className="mt-4 h-4 w-full" />
      <Skeleton className="h-4 w-5/6" />
    </div>
  )
}
