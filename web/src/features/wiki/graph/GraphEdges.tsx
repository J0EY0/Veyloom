import type { CSSProperties } from 'react'
import { BaseEdge, getStraightPath, MarkerType, useInternalNode, type Edge as FlowEdge, type EdgeProps, type EdgeTypes, type InternalNode } from '@xyflow/react'
import type { GraphEdge, GraphEdgeKind } from '@/api/types'
import type { MessageKey } from '@/i18n/zh-CN'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'

// The edges of the relation graph (docs/design.md 5.17). A force layout
// puts nodes on every side of each other, so an edge runs straight from
// one card's border to the other's rather than between handles on their
// left and right.

export interface RelationData extends Record<string, unknown> {
  edge: GraphEdge
  // How the edge stands to the focus: lit when both its ends are near the
  // node in focus, faded when something else is in focus.
  lit: boolean
  faded: boolean
}

export type GraphFlowEdge = FlowEdge<RelationData, 'relation'>

// How each kind is drawn: a link solid, with an arrow to the page it
// leads to; which page took over from which heavier, with the word on it;
// what a page rests on thin; a path named, a topic come from and what a
// directory holds dashed, the way AI Elements draws its temporary edge.
const strokes: Record<GraphEdgeKind, CSSProperties> = {
  link: { strokeWidth: 1.25 },
  supersedes: { strokeWidth: 2 },
  source: { strokeWidth: 0.75 },
  names: { strokeWidth: 1, strokeDasharray: '5 5' },
  from: { strokeWidth: 1, strokeDasharray: '5 5' },
  contains: { strokeWidth: 1, strokeDasharray: '5 5' },
}

const arrowed: GraphEdgeKind[] = ['link', 'supersedes', 'source']

// How each kind reads as a sentence.
export const edgeSentences: Record<GraphEdgeKind, MessageKey> = {
  link: 'wiki.graph.edge.link',
  supersedes: 'wiki.graph.edge.supersedes',
  source: 'wiki.graph.edge.source',
  names: 'wiki.graph.edge.names',
  from: 'wiki.graph.edge.from',
  contains: 'wiki.graph.edge.contains',
}

// toFlowEdge is an edge as React Flow takes it, coloured for the focus,
// and said in words for a screen reader.
export function toFlowEdge(edge: GraphEdge, lit: boolean, faded: boolean, ariaLabel: string): GraphFlowEdge {
  const color = lit ? 'var(--foreground)' : 'var(--ring)'
  return {
    id: `${edge.kind} ${edge.from} ${edge.to}`,
    source: edge.from,
    target: edge.to,
    type: 'relation',
    ariaLabel,
    data: { edge, lit, faded },
    markerEnd: arrowed.includes(edge.kind) ? { type: MarkerType.ArrowClosed, width: 14, height: 14, color } : undefined,
    zIndex: lit ? 1 : 0,
    focusable: false,
    selectable: false,
  }
}

function RelationEdge({ id, source, target, data, markerEnd }: EdgeProps<GraphFlowEdge>) {
  const t = useT()
  const from = useInternalNode(source)
  const to = useInternalNode(target)
  if (!from || !to || !data) return null
  const [sx, sy] = borderPoint(from, to)
  const [tx, ty] = borderPoint(to, from)
  const [path, labelX, labelY] = getStraightPath({ sourceX: sx, sourceY: sy, targetX: tx, targetY: ty })
  const kind = data.edge.kind
  return (
    <g className={cn('transition-opacity', data.faded && 'opacity-15')}>
      <BaseEdge
        id={id}
        path={path}
        markerEnd={markerEnd}
        style={{ ...strokes[kind], stroke: data.lit ? 'var(--foreground)' : 'var(--ring)' }}
        label={kind === 'supersedes' ? t('wiki.graph.kind.supersedes') : undefined}
        labelX={labelX}
        labelY={labelY}
        labelBgPadding={[4, 2]}
        labelBgBorderRadius={4}
      />
    </g>
  )
}

export const edgeTypes: EdgeTypes = { relation: RelationEdge }

// borderPoint is where the line from the node's centre to the other's
// leaves the node's card.
function borderPoint(node: InternalNode, other: InternalNode): [number, number] {
  const [cx, cy] = centre(node)
  const [ox, oy] = centre(other)
  const halfWidth = (node.measured.width ?? 0) / 2
  const halfHeight = (node.measured.height ?? 0) / 2
  const dx = ox - cx
  const dy = oy - cy
  if (dx === 0 && dy === 0) return [cx, cy]
  const scale = Math.min(dx === 0 ? Infinity : halfWidth / Math.abs(dx), dy === 0 ? Infinity : halfHeight / Math.abs(dy))
  return [cx + dx * scale, cy + dy * scale]
}

function centre(node: InternalNode): [number, number] {
  const { x, y } = node.internals.positionAbsolute
  return [x + (node.measured.width ?? 0) / 2, y + (node.measured.height ?? 0) / 2]
}
