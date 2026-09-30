import { useState } from 'react'
import { CheckCheckIcon, ChevronRightIcon, FolderIcon, PenLineIcon, ShieldCheckIcon, TagIcon } from 'lucide-react'
import { Link } from 'react-router'
import type { WikiPage, WikiSource, WikiTeam } from '@/api/types'
import { useWikiHistory, type WikiSpace } from '@/api/wiki'
import { Badge } from '@/components/ui/badge'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Spinner } from '@/components/ui/spinner'
import { formatDay, formatTime } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { CommitRow, type OpenTopic } from './CommitRow'
import { Fact, Facts, TrustPill } from './Facts'
import { pageHref } from './links'
import { actorName, producerName } from './names'

// The parts of a page under its text: what it rests on, what it carries
// besides its text, and how it came to be as it is. How it relates to the
// rest of the wiki is PageRelations'.

export interface PagePartProps {
  page: WikiPage
  space: WikiSpace
  onOpenThread: OpenTopic
}

export function PageSources({ page, space, onOpenThread }: PagePartProps) {
  const t = useT()
  if (page.sources.length === 0) return null
  return (
    <section aria-label={t('wiki.page.sources')} className="mt-8">
      <h2 className="mb-1.5 text-xs font-medium text-subtle">{t('wiki.page.sources')}</h2>
      <ul className="grid gap-1 text-[0.8125rem] sm:grid-cols-2 sm:gap-x-8">
        {page.sources.map((source, index) => (
          <li key={source.id ?? index} className="min-w-0 break-words">
            <SourceLink source={source} space={space} onOpenThread={onOpenThread} />
          </li>
        ))}
      </ul>
    </section>
  )
}

// PageDetails is what a page carries besides its text, quiet at its foot
// where the head leaves it out: where it lies, its tags, how far to trust
// it, and who wrote it and last checked it, with the model. A page shown
// alone, as the library's are, goes by its name, not where it lies.
export function PageDetails({ page, alone = false }: { page: WikiPage; alone?: boolean }) {
  const t = useT()
  const tags = page.tags.filter((tag) => tag.toLowerCase() !== 'resident' && !tag.toLowerCase().startsWith('runtime-'))
  const latest = page.verified.at(-1)
  // The maintainer's confirmation, when it is the latest (confirm_wiki,
  // docs/design.md 5.16).
  const checked = latest && !latest.by.startsWith('human:') ? latest : undefined
  // The byline names who wrote it; here only what it leaves out, the model.
  const model = page.generated_by && page.generated_at && actorName(page.generated_by) !== producerName(page.generated_by)
  return (
    <section aria-label={t('wiki.details.title')} className="mt-8">
      <h2 className="mb-2 text-xs font-medium text-subtle">{t('wiki.details.title')}</h2>
      <Facts>
        {alone ? null : (
          <Fact icon={FolderIcon} label={t('wiki.details.path')}>
            <span className="font-mono text-xs break-all" translate="no">
              {page.path}
            </span>
          </Fact>
        )}
        {tags.length > 0 ? (
          <Fact icon={TagIcon} label={t('wiki.details.tags')}>
            {tags.map((tag) => (
              <Badge key={tag} variant="outline" className="h-6 rounded-full px-2 text-xs font-normal text-muted-foreground">
                {tag}
              </Badge>
            ))}
          </Fact>
        ) : null}
        <Fact icon={ShieldCheckIcon} label={t('wiki.details.trust')}>
          <TrustPill tier={page.tier} />
        </Fact>
        {model ? (
          <Fact icon={PenLineIcon} label={t('wiki.details.written')}>
            {actorName(page.generated_by as string)} · {formatTime(page.generated_at as string)}
          </Fact>
        ) : null}
        {checked ? (
          <Fact icon={CheckCheckIcon} label={t('wiki.details.checked')}>
            {actorName(checked.by)} · {formatTime(checked.at)}
          </Fact>
        ) : null}
      </Facts>
    </section>
  )
}

// A source leads where it says: back to the topic of the chat it came
// from, to another page, or out of Veyloom.
function SourceLink({ source, space, onOpenThread }: { source: WikiSource; space: WikiSpace; onOpenThread: OpenTopic }) {
  const t = useT()
  if (source.message_id) return <FileSource source={source} onOpenThread={onOpenThread} />
  if (source.thread_id && source.topic_number) {
    const label = source.turn_id ? t('wiki.source.turn', { n: source.topic_number }) : t('wiki.source.topic', { n: source.topic_number })
    return (
      <button type="button" onClick={() => onOpenThread(source.thread_id as string, source.room_id)} className="text-left underline-offset-3 hover:underline">
        {label}
      </button>
    )
  }
  if (source.page) {
    return (
      <Link to={pageHref(space, source.page)} className="underline-offset-3 hover:underline">
        {source.title || source.page}
      </Link>
    )
  }
  if (/^https?:\/\//.test(source.resource)) {
    return (
      <a href={source.resource} target="_blank" rel="noreferrer noopener" className="underline-offset-3 hover:underline">
        {source.title || source.resource}
      </a>
    )
  }
  if (source.resource.startsWith('veyloom://')) {
    const missing = source.resource.includes('/messages/') ? t('wiki.source.missingMessage', { name: source.title ?? '' }) : t('wiki.source.missingTopic')
    return <span className="text-muted-foreground">{missing}</span>
  }
  return (
    <span className="text-muted-foreground">
      {source.title ? `${source.title} · ` : ''}
      <span className="font-mono text-xs" translate="no">
        {source.resource}
      </span>
    </span>
  )
}

// A file kept in the wiki names the message it came with: who sent it and
// when, and the topic it came in, which opens; one said in the chat itself
// has nowhere further to lead.
function FileSource({ source, onOpenThread }: { source: WikiSource; onOpenThread: OpenTopic }) {
  const t = useT()
  const vars = { name: source.title ?? '', by: source.sent_by || t('wiki.source.someone'), day: source.sent_at ? formatDay(source.sent_at) : '' }
  if (source.thread_id && source.topic_number) {
    return (
      <button type="button" onClick={() => onOpenThread(source.thread_id as string, source.room_id)} className="text-left underline-offset-3 hover:underline">
        {t('wiki.source.fileInTopic', { ...vars, n: source.topic_number })}
      </button>
    )
  }
  return <span className="text-muted-foreground">{t('wiki.source.fileInChat', vars)}</span>
}

// The page's changes, read when asked for.
// PageHistory is what changed a page, read when opened. Compact, it is a
// row among a skill's facts, which names it already.
export function PageHistory({
  page,
  space,
  canUndo,
  teams,
  onOpenThread,
  compact = false,
}: PagePartProps & { canUndo: boolean; teams?: WikiTeam[]; compact?: boolean }) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const history = useWikiHistory(space, page.path, 30, open)
  const toggle = open ? t('skill.facts.hide') : t('skill.facts.show')
  return (
    <Collapsible open={open} onOpenChange={setOpen} className={compact ? 'group/history' : 'group/history mt-8'}>
      <CollapsibleTrigger
        // Compact, the row's label says what opens; the button says it too.
        aria-label={compact ? `${t('wiki.page.history')} · ${toggle}` : undefined}
        className={
          compact
            ? 'flex items-center gap-1 text-xs leading-6 text-subtle hover:text-foreground'
            : 'flex items-center gap-1 text-xs font-medium text-subtle hover:text-foreground'
        }
      >
        <ChevronRightIcon className="size-3.5 transition-transform group-data-[state=open]/history:rotate-90" />
        {compact ? toggle : t('wiki.page.history')}
      </CollapsibleTrigger>
      <CollapsibleContent>
        {history.isPending ? (
          <p role="status" className="flex items-center gap-2 py-2 text-xs text-subtle">
            <Spinner className="size-3" />
            {t('common.loading')}
          </p>
        ) : (
          <ul className="divide-y">
            {(history.data ?? []).map((commit) => (
              <CommitRow key={commit.sha} commit={commit} space={space} canUndo={canUndo} teams={teams} onOpenThread={onOpenThread} />
            ))}
          </ul>
        )}
      </CollapsibleContent>
    </Collapsible>
  )
}
