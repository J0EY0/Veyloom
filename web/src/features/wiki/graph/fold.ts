import type { GraphEdge, GraphNode, WikiGraph } from '@/api/types'

// Folded is a wiki's graph with the paths of the repository its pages name
// folded into the directories they sit in (docs/webui.md 4.14): one node
// for a directory, and one edge from each page naming a path in it, which
// says the paths. A directory a page names itself stands for itself.
export interface Folded {
  graph: WikiGraph
  // The directory's node each path's folded into, by the path's node.
  into: Map<string, string>
  // The paths pages name in each directory, as they go on from it.
  paths: Map<string, string[]>
}

// The repository's top, where a path with no directory sits.
export const topDir = './'

// directoryOf is the directory a path node folds into, ending in a slash.
export function directoryOf(node: GraphNode): string {
  const path = node.file ?? ''
  if (node.dir) return path.endsWith('/') ? path : `${path}/`
  const at = path.lastIndexOf('/')
  return at === -1 ? topDir : path.slice(0, at + 1)
}

export function foldPaths(graph: WikiGraph): Folded {
  const into = new Map<string, string>()
  const rest = new Map<string, string>()
  const nodes: GraphNode[] = []
  const dirs = new Map<string, GraphNode>()
  for (const node of graph.nodes) {
    if (node.kind !== 'file') {
      nodes.push(node)
      continue
    }
    const dir = directoryOf(node)
    const id = `file:${dir}`
    into.set(node.id, id)
    rest.set(node.id, dir === topDir ? (node.file ?? '') : (node.file ?? '').slice(dir.length))
    if (!dirs.has(id)) {
      const folded: GraphNode = { id, kind: 'file', file: dir, dir: true, pages: 0 }
      dirs.set(id, folded)
      nodes.push(folded)
    }
  }
  // Which paths each page names in each directory.
  const named = new Map<string, Map<string, Set<string>>>()
  const edges: GraphEdge[] = []
  for (const edge of graph.edges) {
    const dir = into.get(edge.to)
    // A directory's place among the paths in it shows in the paths.
    if (edge.kind === 'contains') continue
    if (edge.kind !== 'names' || !dir) {
      edges.push(edge)
      continue
    }
    const byPage = named.get(dir) ?? new Map<string, Set<string>>()
    named.set(dir, byPage)
    const paths = byPage.get(edge.from) ?? new Set<string>()
    byPage.set(edge.from, paths)
    const path = rest.get(edge.to)
    if (path) paths.add(path)
  }
  const paths = new Map<string, string[]>()
  for (const [dir, byPage] of named) {
    const all = new Set<string>()
    for (const [page, named] of byPage) {
      const list = [...named].sort()
      list.forEach((path) => all.add(path))
      edges.push({ from: page, to: dir, kind: 'names', context: list.length > 0 ? list.join(', ') : undefined })
    }
    const node = dirs.get(dir)
    if (node) node.pages = byPage.size
    paths.set(dir, [...all].sort())
  }
  return { graph: { nodes, edges }, into, paths }
}
