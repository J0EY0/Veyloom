import { Fragment, type ReactNode } from 'react'
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
import { contextParts } from './textParts'

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
  // What it shares with the page: paths, topics.
  after?: ReactNode
}

interface Group {
  key: RelationKey
  items: Item[]
}

export interface PageRelationsProps {
  page: WikiPage
  space: WikiSpace
  onOpenThread: OpenTopic
  // Under a skill's files its headings are as quiet as theirs; a page of a
  // project's wiki has them as the overview does, a section each.
  quiet?: boolean
}

// PageRelations lists under a page how it relates to the rest of the wiki
// (docs/design.md 5.17, step 4; docs/webui.md 4.14): the pages it links
// to and those linking to it, each with the sentence the link is in; which
// it took over from or was taken over by; what it rests on and what rests
// on it; and the pages naming a path it names, or come from a topic it came
// from, each once with what it shares. One column, a group under the
// other. Until the graph is read, or for a page it holds no node of (one
// in a skill's folder, which the graph counts as the skill), the page's
// own backlinks stand in.
export function PageRelations({ page, space, onOpenThread, quiet = false }: PageRelationsProps) {
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
  const files = byPage(relations?.files ?? [])
  const topics = byPage(relations?.topics ?? [])
  // A path a sentence links to reads as the page's title.
  const titles = new Map((graph.data?.nodes ?? []).flatMap((node) => (node.page ? [[node.id, node.page.title] as const] : [])))
  const titleOf = (path: string) => titles.get(path)
  if (!inGraph && groups.length === 0) return null
  const empty = groups.length === 0 && files.length === 0 && topics.length === 0
  // A skill seldom links to other pages: with none, it goes without the part.
  if (empty && space.kind === 'library') return null

  return (
    <section aria-labelledby="page-relations" className="mt-8">
      <div className="mb-3 flex items-center gap-2">
        <h2 id="page-relations" className={quiet ? quietHeading : sectionHeading}>
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
      <div className="flex flex-col gap-4">
        {groups.map((group) => (
          <Group key={group.key} label={t(relationNames[group.key])} count={group.items.length}>
            <ItemList items={group.items} space={space} titleOf={titleOf} />
          </Group>
        ))}
        {files.length > 0 ? (
          <Group label={t('wiki.rel.sameFiles')} count={files.length}>
            <ItemList
              items={files.slice(0, sharedMax).map(({ node, via }) => ({
                ...itemOf(node),
                after: (
                  <span className="font-mono text-xs break-all text-subtle" translate="no">
                    {via.map((file) => file.file ?? file.id).join(t('common.listSeparator'))}
                  </span>
                ),
              }))}
              space={space}
              titleOf={titleOf}
            />
            <More n={files.length - sharedMax} />
          </Group>
        ) : null}
        {topics.length > 0 ? (
          <Group label={t('wiki.rel.sameTopics')} count={topics.length}>
            <ItemList
              items={topics.slice(0, sharedMax).map(({ node, via }) => ({
                ...itemOf(node),
                after: via.map((shared, index) =>
                  shared.topic ? (
                    <Fragment key={shared.id}>
                      {index > 0 ? t('common.listSeparator') : null}
                      <button
                        type="button"
                        onClick={() => onOpenThread(shared.topic?.thread_id as string, shared.topic?.room_id)}
                        className="text-xs text-subtle underline-offset-3 hover:text-foreground hover:underline"
                      >
                        {t('wiki.graph.topic', { n: shared.topic.number })}
                      </button>
                    </Fragment>
                  ) : null,
                ),
              }))}
              space={space}
              titleOf={titleOf}
            />
            <More n={topics.length - sharedMax} />
          </Group>
        ) : null}
      </div>
    </section>
  )
}

// The headings of the parts under a page's text: a section each, as the
// overview's (WikiOverview), or as quiet as a skill's list of files.
export const sectionHeading = 'text-sm font-semibold'
export const quietHeading = 'text-xs font-medium text-subtle'

// Group is one kind of relation: its name and how many, then the pages.
function Group({ label, count, children }: { label: string; count: number; children: ReactNode }) {
  return (
    <section aria-label={label}>
      <h3 className="mb-1.5 text-xs text-subtle">
        {label}
        <span className="ml-1.5 tabular-nums">{count}</span>
      </h3>
      {children}
    </section>
  )
}

// ItemList is the pages a relation leads to, a line each: the title, what
// it shares with the page after it, and under it the sentence a link is in.
function ItemList({ items, space, titleOf }: { items: Item[]; space: WikiSpace; titleOf: (path: string) => string | undefined }) {
  const t = useT()
  return (
    <ul className="flex flex-col gap-2 text-[0.8125rem]">
      {items.map((item) => (
        <li key={item.path} className="min-w-0 break-words">
          <Link to={pageHref(space, item.path)} className={cn('underline-offset-3 hover:underline', item.deprecated && 'text-muted-foreground line-through')}>
            {item.title}
          </Link>
          {item.mount ? <span className="text-subtle"> · {t('wiki.mount', { name: item.mount })}</span> : null}
          {item.after ? <span className="text-subtle"> · {item.after}</span> : null}
          {item.context ? (
            <p className="mt-0.5 line-clamp-2 text-xs leading-relaxed text-subtle">
              {contextParts(item.context, titleOf).map((part, index) =>
                part.hit ? (
                  <span key={index} className="text-muted-foreground">
                    {part.text}
                  </span>
                ) : (
                  part.text
                ),
              )}
            </p>
          ) : null}
        </li>
      ))}
    </ul>
  )
}

function More({ n }: { n: number }) {
  const t = useT()
  return n > 0 ? <p className="mt-1.5 text-xs text-subtle">{t('wiki.rel.more', { n })}</p> : null
}

// byPage turns what the page shares, a path or a topic with the pages
// sharing it, around: each page once, with all it shares, the pages
// sharing the most first.
function byPage(shared: SharedGroup[]): { node: GraphNode; via: GraphNode[] }[] {
  const pages = new Map<string, { node: GraphNode; via: GraphNode[] }>()
  for (const group of shared) {
    for (const node of group.pages) {
      const entry = pages.get(node.id) ?? { node, via: [] }
      entry.via.push(group.via)
      pages.set(node.id, entry)
    }
  }
  return [...pages.values()].sort((a, b) => b.via.length - a.via.length || (a.node.page?.title ?? a.node.id).localeCompare(b.node.page?.title ?? b.node.id))
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
