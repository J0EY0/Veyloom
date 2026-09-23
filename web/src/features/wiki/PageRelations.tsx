import type { ReactNode } from 'react'
import { NetworkIcon } from 'lucide-react'
import { Link } from 'react-router'
import type { GraphNode, WikiLink, WikiPage } from '@/api/types'
import { useWikiGraph, type WikiSpace } from '@/api/wiki'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import type { OpenTopic } from './CommitRow'
import { pageRelations, type RelationKey, type SharedGroup } from './graph/model'
import { graphHref, pageHref } from './links'
import { relationNames } from './names'

// How many pages a path or a topic lists under a page; the graph shows
// the rest.
const sharedMax = 8

// A page a relation leads to.
interface Item {
  path: string
  title: string
  // The sentence a link is in.
  context?: string
  mount?: string
  deprecated?: boolean
}

interface Group {
  key: RelationKey
  items: Item[]
}

export interface PageRelationsProps {
  page: WikiPage
  space: WikiSpace
  onOpenThread: OpenTopic
}

// PageRelations lists under a page how it relates to the rest of the wiki
// (docs/design.md 5.17, step 4): the pages it links to and those linking
// to it, with the sentence each link is in; which it took over from or was
// taken over by; what it rests on and what rests on it; and the pages
// naming a path it names, or come from a topic it came from. Until the
// graph is read, or for a page it holds no node of (one in a skill's
// folder, which the graph counts as the skill), the page's own backlinks
// stand in.
export function PageRelations({ page, space, onOpenThread }: PageRelationsProps) {
  const t = useT()
  const graph = useWikiGraph(space)
  const inGraph = graph.data?.nodes.some((node) => node.id === page.path) ?? false
  const relations = graph.data && inGraph ? pageRelations(graph.data, page.path) : undefined
  // A link carries the sentence it is in; a source's context is only the
  // title the page gives it.
  const groups = withBacklinks(
    (relations?.direct ?? []).map((group) => ({
      key: group.key,
      items: group.related.map(({ node, context }) => itemOf(node, group.key === 'linksTo' || group.key === 'linkedFrom' ? context : undefined)),
    })),
    // The graph holds every link within the project's wiki, taking over
    // from another page apart from linking; a mounted page's backlinks
    // within its own bundle it does not.
    !inGraph || page.mount ? page.backlinks : [],
  )
  const files = relations?.files ?? []
  const topics = relations?.topics ?? []
  if (!inGraph && groups.length === 0) return null
  const empty = groups.length === 0 && files.length === 0 && topics.length === 0

  return (
    <section aria-labelledby="page-relations" className="mt-8">
      <div className="mb-2 flex items-center gap-2">
        <h2 id="page-relations" className="text-xs font-medium text-subtle">
          {t('wiki.page.relations')}
        </h2>
        <span className="grow" />
        {inGraph && space.kind === 'project' ? (
          <Link to={graphHref(space, page.path)} className="flex items-center gap-1 text-xs text-subtle hover:text-foreground">
            <NetworkIcon aria-hidden="true" className="size-3.5" />
            {t('wiki.page.inGraph')}
          </Link>
        ) : null}
      </div>
      {empty ? <p className="text-[0.8125rem] text-muted-foreground">{t('wiki.page.noRelations')}</p> : null}
      <div className="grid gap-x-8 gap-y-5 sm:grid-cols-2">
        {groups.map((group) => (
          <section key={group.key} aria-label={t(relationNames[group.key])}>
            <Heading label={t(relationNames[group.key])} count={group.items.length} />
            <ItemList items={group.items} space={space} />
          </section>
        ))}
        {files.length > 0 ? (
          <section aria-label={t('wiki.rel.sameFiles')}>
            <Heading label={t('wiki.rel.sameFiles')} />
            {files.map((shared) => (
              <Shared key={shared.via.id} shared={shared} space={space}>
                <span className="font-mono text-xs break-all" translate="no">
                  {shared.via.file}
                </span>
              </Shared>
            ))}
          </section>
        ) : null}
        {topics.length > 0 ? (
          <section aria-label={t('wiki.rel.sameTopics')}>
            <Heading label={t('wiki.rel.sameTopics')} />
            {topics.map((shared) => {
              const topic = shared.via.topic
              return (
                <Shared key={shared.via.id} shared={shared} space={space}>
                  {topic ? (
                    <button
                      type="button"
                      onClick={() => onOpenThread(topic.thread_id, topic.room_id)}
                      className="text-left text-xs break-words underline-offset-3 hover:underline"
                    >
                      {t('wiki.graph.topic', { n: topic.number })}
                      {topic.title ? ` · ${topic.title}` : ''}
                    </button>
                  ) : null}
                </Shared>
              )
            })}
          </section>
        ) : null}
      </div>
    </section>
  )
}

function Heading({ label, count }: { label: string; count?: number }) {
  return (
    <h3 className="mb-1 text-xs text-subtle">
      {label}
      {count !== undefined ? <span className="ml-1.5 tabular-nums">{count}</span> : null}
    </h3>
  )
}

function ItemList({ items, space }: { items: Item[]; space: WikiSpace }) {
  const t = useT()
  return (
    <ul className="grid gap-1 text-[0.8125rem]">
      {items.map((item) => (
        <li key={item.path} className="min-w-0 break-words">
          <Link to={pageHref(space, item.path)} className={cn('underline-offset-3 hover:underline', item.deprecated && 'text-muted-foreground line-through')}>
            {item.title}
          </Link>
          {item.mount ? <span className="text-subtle"> · {t('wiki.mount', { name: item.mount })}</span> : null}
          {item.context ? <span className="text-subtle"> {item.context}</span> : null}
        </li>
      ))}
    </ul>
  )
}

// Shared is the pages that share one path or topic with the page, under
// it.
function Shared({ shared, space, children }: { shared: SharedGroup; space: WikiSpace; children: ReactNode }) {
  const t = useT()
  const rest = shared.pages.length - sharedMax
  return (
    <div className="mb-2.5 last:mb-0">
      <p className="mb-0.5 text-muted-foreground">{children}</p>
      <ItemList items={shared.pages.slice(0, sharedMax).map((node) => itemOf(node))} space={space} />
      {rest > 0 ? <p className="mt-0.5 text-xs text-subtle">{t('wiki.rel.more', { n: rest })}</p> : null}
    </div>
  )
}

function itemOf(node: GraphNode, context?: string): Item {
  return {
    path: node.page?.path ?? node.id,
    title: node.page?.title ?? node.id,
    context,
    mount: node.page?.mount,
    deprecated: node.page?.status === 'deprecated',
  }
}

// withBacklinks adds backlinks to what links to the page, those not there
// already.
function withBacklinks(groups: Group[], backlinks: WikiLink[]): Group[] {
  const linked = groups.find((group) => group.key === 'linkedFrom')
  const known = new Set(linked?.items.map((item) => item.path))
  const more = backlinks.filter((link) => !known.has(link.path)).map((link) => ({ path: link.path, title: link.title }))
  if (more.length === 0) return groups
  if (linked) return groups.map((group) => (group === linked ? { ...group, items: [...group.items, ...more] } : group))
  const at = groups.findIndex((group) => group.key !== 'linksTo')
  const next = [...groups]
  next.splice(at === -1 ? groups.length : at, 0, { key: 'linkedFrom', items: more })
  return next
}
