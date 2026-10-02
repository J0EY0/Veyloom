import type { CSSProperties } from 'react'
import { useInternalNode, type Edge as FlowEdge, type EdgeProps, type EdgeTypes, type InternalNode } from '@xyflow/react'
import type { GraphEdge, GraphEdgeKind } from '@/api/types'
import type { MessageKey } from '@/i18n/zh-CN'
import { cn } from '@/lib/utils'
import type { Point } from './layout'

// The edges of the relation graph (docs/design.md 5.17, webui.md 4.14):
// a slight curve from dot to dot, without an arrow; the card says which
// way a relation runs. Within a cluster an edge is darker, one to a
// directory lighter; across clusters thin and faint, behind the islands.
// Those of the node lit are dark, the rest fade. Like the dots, a line
// keeps its width on screen at any zoom.

export interface RelationData extends Record<string, unknown> {
  edge: GraphEdge
  // Both its ends in one cluster.
  inside: boolean
  lit: boolean
  faded: boolean
}

export type GraphFlowEdge = FlowEdge<RelationData, 'relation'>

const width = (px: number) => `calc(${px}px * var(--graph-k, 1))`

const strokes: Record<'inside' | 'path' | 'across' | 'lit', CSSProperties> = {
  inside: { stroke: 'var(--graph-inside)', strokeWidth: width(1.35) },
  path: { stroke: 'var(--graph-inside-path)', strokeWidth: width(0.9) },
  across: { stroke: 'var(--graph-across)', strokeWidth: width(0.7) },
  lit: { stroke: 'var(--graph-lit)', strokeWidth: width(1.6) },
}

// How each kind reads as a sentence.
export const edgeSentences: Record<GraphEdgeKind, MessageKey> = {
  link: 'wiki.graph.edge.link',
  supersedes: 'wiki.graph.edge.supersedes',
  source: 'wiki.graph.edge.source',
  names: 'wiki.graph.edge.names',
  from: 'wiki.graph.edge.from',
  contains: 'wiki.graph.edge.contains',
}

// toFlowEdge is an edge as React Flow takes it, drawn for where its ends
// are and for the focus, and said in words for a screen reader.
export function toFlowEdge(edge: GraphEdge, inside: boolean, lit: boolean, faded: boolean, ariaLabel: string): GraphFlowEdge {
  return {
    id: `${edge.kind} ${edge.from} ${edge.to}`,
    source: edge.from,
    target: edge.to,
    type: 'relation',
    ariaLabel,
    data: { edge, inside, lit, faded },
    zIndex: lit ? 1 : 0,
    focusable: false,
    selectable: false,
  }
}

function RelationEdge({ source, target, data }: EdgeProps<GraphFlowEdge>) {
  const from = useInternalNode(source)
  const to = useInternalNode(target)
  if (!from || !to || !data) return null
  const path = data.edge.kind === 'names' || data.edge.kind === 'from'
  const tone = data.lit ? 'lit' : !data.inside ? 'across' : path ? 'path' : 'inside'
  return (
    <path
      d={curve(centre(from), centre(to), data.inside ? 0.1 : 0.16)}
      fill="none"
      strokeLinecap="round"
      className={cn('pointer-events-none transition-[stroke,opacity] duration-150', data.faded && 'opacity-30')}
      style={strokes[tone]}
    />
  )
}

export const edgeTypes: EdgeTypes = { relation: RelationEdge }

// curve is a line from one point to another bowed a little to one side,
// always the same side for the same two in the same order.
export function curve(a: Point, b: Point, bow: number): string {
  const mx = (a.x + b.x) / 2
  const my = (a.y + b.y) / 2
  const dx = b.x - a.x
  const dy = b.y - a.y
  return `M${a.x},${a.y} Q${mx - dy * bow},${my + dx * bow} ${b.x},${b.y}`
}

function centre(node: InternalNode): Point {
  const { x, y } = node.internals.positionAbsolute
  return { x: x + (node.measured.width ?? 0) / 2, y: y + (node.measured.height ?? 0) / 2 }
}
