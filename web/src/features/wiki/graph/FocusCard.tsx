import { FileIcon, FolderIcon, MessagesSquareIcon, PinIcon, XIcon } from 'lucide-react'
import { Link } from 'react-router'
import type { GraphNode, WikiGraph } from '@/api/types'
import type { WikiSpace } from '@/api/wiki'
import { Button } from '@/components/ui/button'
import { Item, ItemContent, ItemDescription, ItemGroup, ItemTitle } from '@/components/ui/item'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { useT, type t as translate } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import type { OpenTopic } from '../CommitRow'
import { pageHref } from '../links'
import { relationNames, typeName } from '../names'
import { reviewText } from '../review'
import { contextParts } from '../textParts'
import { TypeIcon } from './icons'
import { relationsOf } from './model'

export interface FocusCardProps {
  space: WikiSpace
  // The graph as shown: the card lists what is on screen.
  graph: WikiGraph
  id: string
  // The paths pages name in a directory in focus, as they go on from it.
  paths?: string[]
  onFocus: (id: string) => void
  onOpenThread: OpenTopic
  onClose: () => void
}

// FocusCard is what the graph says about the node in focus (docs/design.md
// 5.17): what it is, how it relates to the rest, grouped by kind, links
// with the sentence they are in, the paths named with the page naming
// them, and a way to open the page or the topic. Picking a relation
// focuses on it.
export function FocusCard({ space, graph, id, paths, onFocus, onOpenThread, onClose }: FocusCardProps) {
  const t = useT()
  const node = graph.nodes.find((known) => known.id === id)
  if (!node) return null
  const groups = relationsOf(graph, id)
  // A link written as a page's path, in the sentence a link is in, reads as
  // the page's title, as under a page (PageRelations).
  const titleOf = (path: string) => graph.nodes.find((known) => known.id === path)?.page?.title
  const page = node.page
  const topic = node.topic
  return (
    <section aria-label={label(t, node)} className="flex max-h-full min-h-0 flex-col">
      <header className="flex items-start gap-2 border-b py-2 pr-1.5 pl-3">
        <div className="min-w-0 flex-1 pt-1">
          <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <KindIcon node={node} />
            {kindText(t, node)}
          </p>
          <h2 className={cn('mt-1 text-sm font-semibold break-words', node.file && 'font-mono font-medium')} translate={node.file ? 'no' : undefined}>
            {page?.title ?? (topic ? topic.title || t('wiki.graph.topic', { n: topic.number }) : node.file)}
          </h2>
          {page ? (
            <p className="mt-0.5 font-mono text-[0.6875rem] break-all text-subtle" translate="no">
              {page.path}
            </p>
          ) : null}
          {paths && paths.length > 0 ? (
            <p className="mt-0.5 font-mono text-[0.6875rem] break-all text-subtle" translate="no">
              {paths.join(', ')}
            </p>
          ) : null}
          {page?.description ? <p className="mt-1.5 text-xs leading-relaxed text-muted-foreground">{page.description}</p> : null}
          <Marks node={node} />
        </div>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon-sm" onClick={onClose} aria-label={t('wiki.graph.unfocus')}>
              <XIcon />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t('wiki.graph.unfocus')}</TooltipContent>
        </Tooltip>
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto px-1.5 py-2">
        {groups.length === 0 ? <p className="px-1.5 text-xs text-subtle">{t('wiki.graph.alone')}</p> : null}
        {groups.map((group) => (
          <section key={group.key} aria-label={t(relationNames[group.key])} className="mb-2 last:mb-0">
            <h3 className="px-1.5 pb-1 text-xs font-medium text-subtle">
              {t(relationNames[group.key])}
              <span className="ml-1.5 tabular-nums">{group.related.length}</span>
            </h3>
            <ItemGroup className="gap-0.5">
              {group.related.map(({ node: other, context }) => (
                <Item key={other.id} asChild size="sm" className="flex-nowrap items-start gap-2 px-1.5 py-1 hover:bg-accent/60">
                  <button type="button" onClick={() => onFocus(other.id)} className="w-full text-left">
                    <KindIcon node={other} className="mt-0.5" />
                    <ItemContent className="min-w-0 gap-0.5">
                      <ItemTitle className={cn('text-xs font-normal break-words', other.file && 'font-mono break-all')}>{label(t, other)}</ItemTitle>
                      {context && (group.key === 'names' || group.key === 'namedBy') ? (
                        <ItemDescription className="line-clamp-none font-mono text-[0.6875rem] break-all" translate="no">
                          {context}
                        </ItemDescription>
                      ) : null}
                      {context && (group.key === 'linksTo' || group.key === 'linkedFrom') ? (
                        <ItemDescription className="line-clamp-none text-[0.6875rem] leading-relaxed break-words">
                          {contextParts(context, titleOf).map((part, index) =>
                            part.hit ? (
                              <span key={index} className="text-foreground/80">
                                {part.text}
                              </span>
                            ) : (
                              part.text
                            ),
                          )}
                        </ItemDescription>
                      ) : null}
                    </ItemContent>
                  </button>
                </Item>
              ))}
            </ItemGroup>
          </section>
        ))}
      </div>
      {page || topic ? (
        <footer className="flex gap-2 border-t p-2">
          {page ? (
            <Button asChild size="sm" variant="outline">
              <Link to={pageHref(space, page.path)}>{t('wiki.graph.openPage')}</Link>
            </Button>
          ) : null}
          {topic ? (
            <Button size="sm" variant="outline" onClick={() => onOpenThread(topic.thread_id, topic.room_id)}>
              {t('wiki.graph.openTopic')}
            </Button>
          ) : null}
        </footer>
      ) : null}
    </section>
  )
}

type T = typeof translate

// label is how a node is named in a list: a page by its title, a path as
// written, a topic by its number and first line.
export function label(t: T, node: GraphNode): string {
  if (node.page) return node.page.title
  if (node.topic) return `${t('wiki.graph.topic', { n: node.topic.number })}${node.topic.title ? ` ${node.topic.title}` : ''}`
  return node.file ?? node.id
}

// kindText is what kind of node it is: a page's type, a directory, a topic.
export function kindText(t: T, node: GraphNode): string {
  if (node.page) return [node.page.mount ? t('wiki.mount', { name: node.page.mount }) : '', typeName(t, node.page.type)].filter(Boolean).join(' · ')
  if (node.topic) return t('wiki.graph.topic', { n: node.topic.number })
  return node.dir ? t('wiki.graph.dir') : t('wiki.graph.file')
}

export function KindIcon({ node, className }: { node: GraphNode; className?: string }) {
  const classes = cn('size-3.5 flex-none text-muted-foreground', className)
  if (node.page) return <TypeIcon type={node.page.type} aria-hidden="true" className={classes} />
  const Icon = node.topic ? MessagesSquareIcon : node.dir ? FolderIcon : FileIcon
  return <Icon aria-hidden="true" className={classes} />
}

// Marks are what the node on the graph shows as signs, said in words.
function Marks({ node }: { node: GraphNode }) {
  const t = useT()
  const page = node.page
  const marks: string[] = []
  if (page?.status === 'deprecated') marks.push(t('wiki.status.deprecated'))
  if (node.kind === 'file' && node.pages) marks.push(t('wiki.graph.namedBy', { n: node.pages }))
  const due = page ? reviewText(t, page) : ''
  if (!page?.resident && marks.length === 0 && !due) return null
  return (
    <div className="mt-1.5 flex flex-col gap-1 text-xs">
      {page?.resident || marks.length > 0 ? (
        <p className="flex flex-wrap items-center gap-x-2 gap-y-1 text-muted-foreground">
          {page?.resident ? (
            <span className="flex items-center gap-1">
              <PinIcon aria-hidden="true" className="size-3" />
              {t('wiki.resident')}
            </span>
          ) : null}
          {marks.map((mark) => (
            <span key={mark}>{mark}</span>
          ))}
        </p>
      ) : null}
      {due ? (
        <p className="flex items-start gap-1.5 text-status-wait">
          <span aria-hidden="true" className="mt-1.5 size-1.5 flex-none rounded-full bg-status-wait" />
          <span className="break-words">
            {t('wiki.review.due')} · {due}
          </span>
        </p>
      ) : null}
    </div>
  )
}
