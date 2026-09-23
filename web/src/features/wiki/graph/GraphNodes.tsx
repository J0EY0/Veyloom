import { memo, useContext, type ReactNode } from 'react'
import type { Node as FlowNode, NodeProps, NodeTypes } from '@xyflow/react'
import { FileIcon, FolderIcon, MessagesSquareIcon, PinIcon } from 'lucide-react'
import type { GraphNode } from '@/api/types'
import { Node, NodeContent } from '@/components/ai-elements/node'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { fades, GraphFocusContext } from './focus'
import { TypeIcon } from './icons'

// The nodes of the relation graph (docs/design.md 5.17): a page is a small
// card with its type's icon and its title, a path of the repository its
// path in monospace, a topic its number. Everything reads on the node
// itself; nothing waits for a hover.

export type GraphFlowNode = FlowNode<{ node: GraphNode }, GraphNode['kind']>

// Card is the AI Elements node every kind is drawn on, shrunk to its
// words. Edges run between the cards' borders, so the handles it carries
// for React Flow stay out of sight.
function Card({ id, className, children }: { id: string; className?: string; children: ReactNode }) {
  const focus = useContext(GraphFocusContext)
  return (
    <Node
      handles={{ target: true, source: true }}
      className={cn(
        'w-auto max-w-56 shadow-none transition-opacity [&_[data-handlepos]]:invisible',
        id === focus.focus && 'border-ring ring-2 ring-ring/40',
        fades(focus, id) && 'opacity-25',
        className,
      )}
    >
      <NodeContent className="flex items-center gap-1.5 px-2.5 py-1.5 text-xs leading-snug">{children}</NodeContent>
    </Node>
  )
}

function PageNode({ id, data }: NodeProps<GraphFlowNode>) {
  const t = useT()
  const page = data.node.page
  if (!page) return null
  const deprecated = page.status === 'deprecated'
  return (
    <Card id={id} className={cn(data.node.kind === 'external' && 'border-dashed', deprecated && 'text-muted-foreground')}>
      <TypeIcon type={page.type} aria-hidden="true" className="size-3.5 flex-none text-muted-foreground" />
      <span className="min-w-0">
        {page.mount ? <span className="block text-[0.6875rem] break-words text-subtle">{t('wiki.mount', { name: page.mount })}</span> : null}
        <span className={cn('line-clamp-2 font-medium break-words', deprecated && 'line-through')}>{page.title}</span>
      </span>
      {page.resident ? (
        <>
          <PinIcon aria-hidden="true" className="size-3 flex-none text-subtle" />
          <span className="sr-only">{t('wiki.resident')}</span>
        </>
      ) : null}
      {page.review ? (
        <>
          <span aria-hidden="true" className="size-1.5 flex-none rounded-full bg-status-wait" />
          <span className="sr-only">{t('wiki.review.due')}</span>
        </>
      ) : null}
    </Card>
  )
}

function FileNode({ id, data }: NodeProps<GraphFlowNode>) {
  const Icon = data.node.dir ? FolderIcon : FileIcon
  return (
    <Card id={id} className="bg-muted">
      <Icon aria-hidden="true" className="size-3.5 flex-none text-muted-foreground" />
      <span className="min-w-0 font-mono break-all" translate="no">
        {data.node.file}
      </span>
    </Card>
  )
}

function TopicNode({ id, data }: NodeProps<GraphFlowNode>) {
  const t = useT()
  return (
    <Card id={id} className="rounded-full">
      <MessagesSquareIcon aria-hidden="true" className="size-3.5 flex-none text-muted-foreground" />
      <span className="font-medium whitespace-nowrap">{t('wiki.graph.topic', { n: data.node.topic?.number ?? 0 })}</span>
    </Card>
  )
}

export const nodeTypes: NodeTypes = {
  page: memo(PageNode),
  external: memo(PageNode),
  file: memo(FileNode),
  topic: memo(TopicNode),
}

// nodeRadius is roughly how far a node reaches from its centre, in rem,
// for the layout to keep nodes apart before they are drawn: half the width
// its words take, within the card's width.
export function nodeRadius(node: GraphNode): number {
  // "话题 #12", "Topic #12": about four and a half ems either way.
  const ems = node.topic ? 4.5 : textEms(node.page?.title ?? node.file ?? '')
  // Text is 0.75rem; the icon, gaps and padding take about 2.5rem more,
  // a mounted page's second line a little height; the card stops at 14rem.
  const width = Math.min(14, ems * 0.75 + 2.5)
  return width / 2 + (node.page?.mount ? 0.5 : 0)
}

// textEms is how many ems text takes: a CJK character a full em, anything
// else a little over half.
function textEms(text: string): number {
  let ems = 0
  for (const char of text) ems += /[\u2e80-\u9fff\uac00-\ud7af\uff00-\uffef]/.test(char) ? 1 : 0.58
  return ems
}
