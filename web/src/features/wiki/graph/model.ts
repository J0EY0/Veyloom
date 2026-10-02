import type { GraphEdgeKind, GraphNode, WikiGraph } from '@/api/types'

// What the relation graph shows (docs/design.md 5.17, webui.md 4.14): the
// part of a wiki's graph a person's filters let through, the pages about
// the one in focus, and how a node relates to the rest. The paths are
// folded into their directories first (fold.ts); where nodes go is
// layout.ts's.

// Which nodes for the repository's directories: those two or more pages
// shown name a path in (the default), all of them, or none.
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

// The defaults the user settled on: directories two pages share, no
// topics.
export const defaultFilters: GraphFilters = { hiddenTypes: [], deprecated: false, files: 'shared', topics: false, external: true, depth: 1, hiddenKinds: [] }

// visibleGraph is what the filters let through: the nodes, and the edges
// between them. A directory or topic stands for nothing without the pages
// shown that name a path in it or came from it.
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

// steps is how far each node within depth steps of the one in focus is
// from it, whichever way the edges run: the node itself 0 steps.
export function steps(graph: WikiGraph, focus: string, depth: number): Map<string, number> {
  const seen = new Map([[focus, 0]])
  let frontier = new Set([focus])
  for (let step = 1; step <= depth; step++) {
    const next = new Set<string>()
    for (const edge of graph.edges) {
      for (const [from, to] of [
        [edge.from, edge.to],
        [edge.to, edge.from],
      ]) {
        if (frontier.has(from) && !seen.has(to)) {
          seen.set(to, step)
          next.add(to)
        }
      }
    }
    frontier = next
  }
  return seen
}

// neighborhood is the node in focus and those up to depth steps from it.
export function neighborhood(graph: WikiGraph, focus: string, depth: number): Set<string> {
  return new Set(steps(graph, focus, depth).keys())
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
