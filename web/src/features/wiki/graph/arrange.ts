import type { WikiGraph } from '@/api/types'
import type { t as translate } from '@/lib/i18n'
import { kindText, label } from './FocusCard'
import type { GraphFlowNode } from './GraphNodes'
import { designRem, glyphOf } from './glyphs'
import { clusterGraph, degrees, generalCluster, layout, type Clustering, type Point } from './layout'

type T = typeof translate

// Arranged is a layout of the graph the filters let through: its
// clusters, how many edges each node has, and where each node goes, in
// the canvas's units: design px at the interface's rem.
export interface Arranged {
  signature: string
  clustering: Clustering
  degree: Map<string, number>
  points: Map<string, Point>
}

export function arrange(graph: WikiGraph, signature: string, rem: number): Arranged {
  const clustering = clusterGraph(graph)
  const scale = rem / designRem
  const points = new Map([...layout(graph, clustering)].map(([id, { x, y }]) => [id, { x: x * scale, y: y * scale }]))
  return { signature, clustering, degree: degrees(graph), points }
}

// toNodes is the layout as React Flow's nodes, each at its centre, named
// for a screen reader. The clusters fade in from the middle out.
export function toNodes(arranged: Arranged, graph: WikiGraph, t: T): GraphFlowNode[] {
  const { clustering, points, degree } = arranged
  const middle = (members: string[]) => {
    const at = members.flatMap((id) => points.get(id) ?? [])
    return Math.hypot(at.reduce((sum, p) => sum + p.x, 0) / Math.max(1, at.length), at.reduce((sum, p) => sum + p.y, 0) / Math.max(1, at.length))
  }
  const order = new Map(
    [...clustering.clusters]
      .map((cluster) => ({ id: cluster.id, distance: middle(cluster.members) }))
      .sort((a, b) => a.distance - b.distance)
      .map((cluster, index) => [cluster.id, index]),
  )
  return graph.nodes.map((node) => {
    const cluster = clustering.of.get(node.id) ?? generalCluster
    return {
      id: node.id,
      type: 'dot',
      position: points.get(node.id) ?? { x: 0, y: 0 },
      data: { node, glyph: glyphOf(node, cluster === node.id), degree: degree.get(node.id) ?? 0, order: order.get(cluster) ?? 0 },
      ariaLabel: t('wiki.graph.nodeLabel', { kind: kindText(t, node), name: label(t, node) }),
      className: 'group outline-none',
    }
  })
}
