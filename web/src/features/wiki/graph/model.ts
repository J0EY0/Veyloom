import { forceCollide, forceLink, forceManyBody, forceSimulation, forceX, forceY, type SimulationLinkDatum, type SimulationNodeDatum } from 'd3-force'
import type { GraphEdgeKind, GraphNode, WikiGraph } from '@/api/types'

// What the relation graph shows (docs/design.md 5.17): the part of a wiki's
// graph a person's filters let through, the pages about the one in focus,
// and where each node goes.

// Which nodes for paths of the repository: those two or more pages shown
// name (the default), all of them, or none.
export type FileNodes = 'shared' | 'all' | 'none'

export interface GraphFilters {
  // Types of page left out.
  hiddenTypes: string[]
  deprecated: boolean
  files: FileNodes
  topics: boolean
  external: boolean
  // How far the focus reaches: the pages one or two steps from it.
  depth: 1 | 2
  // Kinds of relation left out.
  hiddenKinds: GraphEdgeKind[]
}

// The defaults the user settled on: paths two pages share, no topics.
export const defaultFilters: GraphFilters = { hiddenTypes: [], deprecated: false, files: 'shared', topics: false, external: true, depth: 1, hiddenKinds: [] }

// visibleGraph is what the filters let through: the nodes, and the edges
// between them. A path or topic stands for nothing without the pages shown
// that name it or came from it.
export function visibleGraph(graph: WikiGraph, filters: GraphFilters): WikiGraph {
  const pages = new Set<string>()
  for (const node of graph.nodes) {
    const typed = !filters.hiddenTypes.includes(node.page?.type ?? '')
    const shown =
      (node.kind === 'page' && typed && (filters.deprecated || node.page?.status !== 'deprecated')) || (node.kind === 'external' && typed && filters.external)
    if (shown) pages.add(node.id)
  }
  const edges = graph.edges.filter((edge) => !filters.hiddenKinds.includes(edge.kind))
  const reached = new Map<string, number>()
  for (const edge of edges) {
    if ((edge.kind === 'names' || edge.kind === 'from') && pages.has(edge.from)) reached.set(edge.to, (reached.get(edge.to) ?? 0) + 1)
  }
  const shown = new Set(pages)
  for (const node of graph.nodes) {
    const by = reached.get(node.id) ?? 0
    if (node.kind === 'topic' && filters.topics && by > 0) shown.add(node.id)
    if (node.kind === 'file' && by >= (filters.files === 'all' ? 1 : filters.files === 'shared' ? 2 : Infinity)) shown.add(node.id)
  }
  return {
    nodes: graph.nodes.filter((node) => shown.has(node.id)),
    edges: edges.filter((edge) => shown.has(edge.from) && shown.has(edge.to) && edge.from !== edge.to),
  }
}

// filtersShowing are the filters with as little changed as it takes for
// them to show the node, one a link asks to focus on.
export function filtersShowing(graph: WikiGraph, filters: GraphFilters, id: string): GraphFilters {
  const node = graph.nodes.find((known) => known.id === id)
  if (!node) return filters
  const next = { ...filters, hiddenTypes: filters.hiddenTypes.filter((type) => type !== node.page?.type) }
  if (node.kind === 'page' && node.page?.status === 'deprecated') next.deprecated = true
  if (node.kind === 'external') next.external = true
  if (node.kind === 'topic') next.topics = true
  if (node.kind === 'file' && !visibleGraph(graph, next).nodes.some((shown) => shown.id === id)) next.files = 'all'
  return next
}

// neighborhood is the node in focus and those up to depth steps from it,
// whichever way the edges run.
export function neighborhood(graph: WikiGraph, focus: string, depth: number): Set<string> {
  const near = new Set([focus])
  let frontier = [focus]
  for (let step = 0; step < depth; step++) {
    const next: string[] = []
    for (const edge of graph.edges) {
      for (const [from, to] of [
        [edge.from, edge.to],
        [edge.to, edge.from],
      ]) {
        if (frontier.includes(from) && !near.has(to)) {
          near.add(to)
          next.push(to)
        }
      }
    }
    frontier = next
  }
  return near
}

// A relation of a node, as the panel beside the graph and a page list them
// (docs/design.md 5.17): the kind of edge and which way it runs from the
// node. Links to and from come first, as they carry the sentence they
// are in; then which page took over from which, what rests on what, the
// paths named, the topics, and a directory's place among paths.
export const relationKinds = [
  { key: 'linksTo', kind: 'link', out: true },
  { key: 'linkedFrom', kind: 'link', out: false },
  { key: 'supersedes', kind: 'supersedes', out: true },
  { key: 'supersededBy', kind: 'supersedes', out: false },
  { key: 'restsOn', kind: 'source', out: true },
  { key: 'restedOnBy', kind: 'source', out: false },
  { key: 'names', kind: 'names', out: true },
  { key: 'namedBy', kind: 'names', out: false },
  { key: 'cameFrom', kind: 'from', out: true },
  { key: 'gave', kind: 'from', out: false },
  { key: 'within', kind: 'contains', out: false },
  { key: 'holds', kind: 'contains', out: true },
] as const satisfies readonly { key: string; kind: GraphEdgeKind; out: boolean }[]

export type RelationKey = (typeof relationKinds)[number]['key']

export interface Related {
  node: GraphNode
  // The sentence a link is in; a source's title.
  context?: string
}

export interface RelationGroup {
  key: RelationKey
  related: Related[]
}

// relationsOf is how the node relates to the rest of the graph, one group
// per kind of relation it has, each group by name.
export function relationsOf(graph: WikiGraph, id: string): RelationGroup[] {
  const nodes = new Map(graph.nodes.map((node) => [node.id, node]))
  const groups: RelationGroup[] = []
  for (const { key, kind, out } of relationKinds) {
    const related: Related[] = []
    for (const edge of graph.edges) {
      if (edge.kind !== kind || (out ? edge.from : edge.to) !== id) continue
      const node = nodes.get(out ? edge.to : edge.from)
      if (node) related.push({ node, context: edge.context })
    }
    if (related.length > 0) groups.push({ key, related: related.sort((a, b) => nodeName(a.node).localeCompare(nodeName(b.node))) })
  }
  return groups
}

// The relations a page lists under its text (docs/design.md 5.17): how it
// links, which page took over from which, what rests on what; and the
// pages that name a path it names, or came from a topic it came from,
// under that path or topic.
export interface PageRelations {
  direct: RelationGroup[]
  files: SharedGroup[]
  topics: SharedGroup[]
}

// SharedGroup is the other pages sharing a path or a topic with a page.
export interface SharedGroup {
  via: GraphNode
  pages: GraphNode[]
}

const directKinds: RelationKey[] = ['linksTo', 'linkedFrom', 'supersedes', 'supersededBy', 'restsOn', 'restedOnBy']

// pageRelations is what a page lists of its relations.
export function pageRelations(graph: WikiGraph, id: string): PageRelations {
  const groups = relationsOf(graph, id)
  const shared = (key: RelationKey, back: RelationKey): SharedGroup[] =>
    (groups.find((group) => group.key === key)?.related ?? []).flatMap(({ node: via }) => {
      const pages = (relationsOf(graph, via.id).find((group) => group.key === back)?.related ?? [])
        .map((related) => related.node)
        .filter((node) => node.id !== id && node.page)
      return pages.length > 0 ? [{ via, pages }] : []
    })
  return {
    direct: groups.filter((group) => directKinds.includes(group.key)),
    files: shared('names', 'namedBy'),
    topics: shared('cameFrom', 'gave'),
  }
}

// nodeName is what a node is called: a page's title, a path, a topic's
// number.
export function nodeName(node: GraphNode): string {
  if (node.page) return node.page.title
  if (node.topic) return `#${node.topic.number}`
  return node.file ?? node.id
}

export interface Point {
  x: number
  y: number
}

type Placed = SimulationNodeDatum & { id: string; r: number }

// layout places the nodes, each at its centre, the way a force layout
// settles them: linked nodes near each other, none overlapping, pulled a
// little harder to the middle up and down than across, as screens are
// wider than they are tall. It starts
// every node where its id puts it and draws its chance from a fixed seed,
// so the same wiki lays out the same every time.
export function layout(graph: WikiGraph, radius: (node: GraphNode) => number): Map<string, Point> {
  const nodes: Placed[] = graph.nodes.map((node) => ({ id: node.id, r: radius(node), ...seed(node.id, graph.nodes.length) }))
  const links: SimulationLinkDatum<Placed>[] = graph.edges.map((edge) => ({ source: edge.from, target: edge.to }))
  const simulation = forceSimulation(nodes)
    .randomSource(random(1))
    .force(
      'link',
      forceLink<Placed, SimulationLinkDatum<Placed>>(links)
        .id((node) => node.id)
        .distance((link) => (link.source as Placed).r + (link.target as Placed).r + 40),
    )
    .force('charge', forceManyBody<Placed>().strength(-400))
    .force(
      'collide',
      forceCollide<Placed>((node) => node.r + 12),
    )
    .force('x', forceX<Placed>(0).strength(0.03))
    .force('y', forceY<Placed>(0).strength(0.08))
    .stop()
  simulation.tick(300)
  return new Map(nodes.map((node) => [node.id, { x: node.x ?? 0, y: node.y ?? 0 }]))
}

// seed is where a node starts: on a spiral, at a turn its id picks.
function seed(id: string, count: number): Point {
  let hash = 2166136261
  for (let i = 0; i < id.length; i++) {
    hash = Math.imul(hash ^ id.charCodeAt(i), 16777619)
  }
  const unit = (hash >>> 0) / 4294967296
  const angle = unit * Math.PI * 2
  const distance = 40 * Math.sqrt(count) * (0.5 + unit)
  return { x: Math.cos(angle) * distance, y: Math.sin(angle) * distance }
}

// random is a small seeded generator, for what the layout leaves to chance.
function random(seed: number): () => number {
  let state = seed >>> 0
  return () => {
    state = (Math.imul(state, 1664525) + 1013904223) >>> 0
    return state / 4294967296
  }
}
