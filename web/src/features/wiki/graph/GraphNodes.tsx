import { memo, useContext, type CSSProperties } from 'react'
import { Handle, Position, type Node as FlowNode, type NodeProps, type NodeTypes } from '@xyflow/react'
import { MessagesSquareIcon } from 'lucide-react'
import type { GraphNode } from '@/api/types'
import { cn } from '@/lib/utils'
import { fades, GraphViewContext } from './focus'
import { designRem, dotRadius, nameSize, pathSize, regionSize, type Glyph } from './glyphs'
import { TypeIcon } from './icons'
import { isPath, nameGap, nameLine, regionLine, sideGap, type NameLabel, type RegionLabel } from './labels'

// The nodes of the relation graph (docs/design.md 5.17, webui.md 4.14): a
// dot each, the name beside it when it has room, a cluster's name over its
// module. Dots and names keep their size on screen at any zoom: each node
// is a speck on the canvas, and what it draws is scaled back by the zoom
// (--graph-k, set on the canvas as it zooms), so zooming changes only how
// far apart the dots are.

export interface DotData extends Record<string, unknown> {
  node: GraphNode
  glyph: Glyph
  degree: number
  // When its cluster fades in as the graph opens: the middle one first.
  order: number
}

export type GraphFlowNode = FlowNode<DotData, 'dot'>

// rem is a length in design px as rem, so the interface size reaches it.
const rem = (px: number) => `${px / designRem}rem`

// box is a square round the node's centre, so far out from it.
function box(radius: number): CSSProperties {
  return { left: rem(-radius), top: rem(-radius), width: rem(2 * radius), height: rem(2 * radius) }
}

// How each glyph is drawn, round its radius: its fill and its ring, in
// design px.
const glyphs: Record<Glyph, { className: string; ring: number }> = {
  hub: { className: 'rounded-full border-(--graph-halo) bg-(image:--graph-hub) shadow-(--graph-dot-shadow)', ring: 2.5 },
  page: { className: 'rounded-full border-(--graph-halo) bg-(--graph-leaf) shadow-(--graph-dot-shadow)', ring: 1.8 },
  hollow: { className: 'rounded-full border-(--graph-ring) bg-(--graph-halo)', ring: 1.7 },
  external: { className: 'rounded-full border-dashed border-(--graph-ring) bg-(--graph-halo)', ring: 1.5 },
  dir: { className: 'rounded-[0.125rem] border-(--graph-dir) bg-(--graph-halo)', ring: 1.3 },
  topic: { className: 'rounded-full border-(--graph-ring) bg-(--graph-halo)', ring: 1.5 },
}

// Every dot takes a click this far from its centre, however small it is.
const hitRadius = 12

function Dot({ id, data }: NodeProps<GraphFlowNode>) {
  const view = useContext(GraphViewContext)
  const { node, glyph, degree, order } = data
  const r = dotRadius(glyph, degree)
  const name = view.labels.names.get(id)
  const region = view.labels.regions.get(id)
  const lit = view.lead !== '' && (view.near.has(id) || id === view.hover)
  const { className, ring } = glyphs[glyph]
  return (
    <div className="size-0.5">
      <Handle type="target" position={Position.Top} isConnectable={false} className="invisible" />
      <Handle type="source" position={Position.Bottom} isConnectable={false} className="invisible" />
      <div
        className={cn('absolute top-1/2 left-1/2 size-0', view.shown && 'motion-safe:animate-[graph-in_400ms_ease-out_both]')}
        style={{ transform: 'scale(var(--graph-k, 1))', animationDelay: `${order * 70}ms` }}
      >
        {region ? <RegionName label={region} /> : null}
        <div className={cn('transition-opacity duration-150', fades(view, id) && 'opacity-(--graph-dim)')}>
          <span aria-hidden="true" className="absolute cursor-pointer rounded-full" style={box(Math.max(hitRadius, r + 4))} />
          {node.page?.review ? (
            <span aria-hidden="true" className="absolute rounded-full border-status-wait" style={{ ...box(r + 3.4), borderWidth: rem(1.6) }} />
          ) : null}
          {id === view.lead ? (
            <span aria-hidden="true" className="absolute rounded-full border-foreground" style={{ ...box(r + 6), borderWidth: rem(1.6) }} />
          ) : null}
          {/* The node is a speck the browser would ring: the ring goes round the dot instead. */}
          <span aria-hidden="true" className="absolute hidden rounded-full ring-2 ring-ring group-focus-visible:block" style={box(r + 4)} />
          <span aria-hidden="true" className={cn('pointer-events-none absolute', className)} style={{ ...box(r + ring / 2), borderWidth: rem(ring) }} />
          {name ? <Name node={node} glyph={glyph} r={r} label={name} lit={lit} lead={id === view.lead} /> : null}
        </div>
      </div>
    </div>
  )
}

// Where a name goes from the dot's centre: under it, over it, or beside it.
function nameSpot(side: NameLabel['side'], r: number): CSSProperties {
  switch (side) {
    case 'below':
      return { left: 0, top: rem(r + nameGap), transform: 'translateX(-50%)' }
    case 'above':
      return { left: 0, bottom: rem(r + nameGap), transform: 'translateX(-50%)' }
    case 'right':
      return { left: rem(r + sideGap), top: 0, transform: 'translateY(-50%)' }
    case 'left':
      return { right: rem(r + sideGap), top: 0, transform: 'translateY(-50%)' }
  }
}

// A node's name, after its type's icon for a page or a topic, a
// directory's in monospace. The node itself is named for a screen reader;
// this is the same name drawn.
function Name({ node, glyph, r, label, lit, lead }: { node: GraphNode; glyph: Glyph; r: number; label: NameLabel; lit: boolean; lead: boolean }) {
  const page = node.page
  const path = isPath(glyph)
  const icon = cn('flex-none', lit ? 'text-muted-foreground' : 'text-(--graph-icon)')
  const iconSize = { width: rem(nameSize), height: rem(nameSize) }
  return (
    <span
      aria-hidden="true"
      className={cn(
        'absolute flex cursor-pointer items-center whitespace-nowrap transition-colors duration-150 graph-halo',
        path && 'font-mono',
        lit ? 'text-body' : path ? 'text-(--graph-icon)' : 'text-muted-foreground',
        lead && 'font-semibold',
      )}
      style={{ ...nameSpot(label.side, r), fontSize: rem(path ? pathSize : nameSize), lineHeight: rem(nameLine), gap: rem(4) }}
    >
      {page ? <TypeIcon type={page.type} className={icon} style={iconSize} /> : node.topic ? <MessagesSquareIcon className={icon} style={iconSize} /> : null}
      <span className={cn(page?.status === 'deprecated' && 'line-through')} translate={path ? 'no' : undefined}>
        {label.text}
      </span>
    </span>
  )
}

// A cluster's name, large and faint, over its region of the graph.
function RegionName({ label }: { label: RegionLabel }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        'pointer-events-none absolute font-semibold tracking-[0.08em] whitespace-nowrap text-(--graph-region) transition-opacity duration-150 graph-halo-wide',
        label.dim && 'opacity-45',
      )}
      style={{ left: rem(label.dx), top: rem(label.dy), transform: 'translate(-50%, -50%)', fontSize: rem(regionSize), lineHeight: rem(regionLine) }}
    >
      {label.name}
    </span>
  )
}

export const nodeTypes: NodeTypes = { dot: memo(Dot) }
