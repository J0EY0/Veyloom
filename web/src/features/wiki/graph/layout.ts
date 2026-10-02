import { forceCollide, forceLink, forceManyBody, forceSimulation, forceX, forceY, type SimulationLinkDatum, type SimulationNodeDatum } from 'd3-force'
import type { GraphEdgeKind, GraphNode, WikiGraph } from '@/api/types'
import { dotRadius, glyphOf, nameCap, nameSize, textWidth } from './glyphs'

// Where the relation graph's nodes go (docs/webui.md 4.14): the clusters
// pages fall into, a module's and the general one, and a layout of islands.

// A cluster is a module and the pages about it, or the pages about no
// module, the general cluster (docs/webui.md 4.14).
export interface Cluster {
  // The module's node; generalCluster for the general one.
  id: string
  members: string[]
}

export const generalCluster = ''

export interface Clustering {
  clusters: Cluster[]
  // The cluster each node is in.
  of: Map<string, string>
}

// The relations that tie a page to another: a link, which took over from
// which, what rests on what.
const ties: GraphEdgeKind[] = ['link', 'supersedes', 'source']

// How far a page looks along its relations for a module to belong to.
const moduleReach = 3

// isModule says whether a node is a module of the project's: a page of its
// own wiki of type Module, not deprecated.
export function isModule(node: GraphNode): boolean {
  return node.kind === 'page' && node.page?.type === 'Module' && node.page.status !== 'deprecated'
}

// clusterGraph puts each node in a cluster: a module in its own; a page in
// the module it links to, or else the one nearest along its relations,
// three steps at most, or else the general cluster; a directory or topic
// with most of the pages naming it or come from it. Ties go to what comes
// first, in the graph's own order.
export function clusterGraph(graph: WikiGraph): Clustering {
  const modules = new Set(graph.nodes.filter(isModule).map((node) => node.id))
  const out = new Map<string, string[]>()
  const around = new Map<string, string[]>()
  const add = (map: Map<string, string[]>, from: string, to: string) => map.set(from, [...(map.get(from) ?? []), to])
  for (const edge of graph.edges) {
    if (!ties.includes(edge.kind)) continue
    add(out, edge.from, edge.to)
    add(around, edge.from, edge.to)
    add(around, edge.to, edge.from)
  }
  const of = new Map<string, string>()
  for (const node of graph.nodes) {
    if (!node.page) continue
    if (modules.has(node.id)) of.set(node.id, node.id)
    else of.set(node.id, (out.get(node.id) ?? []).find((to) => modules.has(to)) ?? nearest(node.id, around, modules) ?? generalCluster)
  }
  const votes = new Map<string, Map<string, number>>()
  for (const edge of graph.edges) {
    const cluster = of.get(edge.from)
    if ((edge.kind !== 'names' && edge.kind !== 'from') || cluster === undefined || of.has(edge.to)) continue
    const counts = votes.get(edge.to) ?? new Map<string, number>()
    votes.set(edge.to, counts.set(cluster, (counts.get(cluster) ?? 0) + 1))
  }
  for (const node of graph.nodes) {
    if (of.has(node.id)) continue
    let best: [string, number] = [generalCluster, 0]
    for (const vote of votes.get(node.id) ?? []) if (vote[1] > best[1]) best = vote
    of.set(node.id, best[0])
  }
  const clusters: Cluster[] = [...modules].map((id) => ({ id, members: [] }))
  const general: Cluster = { id: generalCluster, members: [] }
  const byId = new Map(clusters.map((cluster) => [cluster.id, cluster]))
  for (const node of graph.nodes) (byId.get(of.get(node.id) ?? generalCluster) ?? general).members.push(node.id)
  return { clusters: general.members.length > 0 ? [...clusters, general] : clusters, of }
}

// nearest is the first module a breadth-first walk from a page meets.
function nearest(from: string, around: Map<string, string[]>, modules: Set<string>): string | undefined {
  const seen = new Set([from])
  let frontier = [from]
  for (let step = 0; step < moduleReach; step++) {
    const next: string[] = []
    for (const id of frontier) {
      for (const other of around.get(id) ?? []) {
        if (seen.has(other)) continue
        if (modules.has(other)) return other
        seen.add(other)
        next.push(other)
      }
    }
    frontier = next
  }
  return undefined
}

// degrees is how many edges each node has.
export function degrees(graph: WikiGraph): Map<string, number> {
  const degree = new Map<string, number>()
  for (const edge of graph.edges) for (const end of [edge.from, edge.to]) degree.set(end, (degree.get(end) ?? 0) + 1)
  return degree
}

export interface Point {
  x: number
  y: number
}

type Placed = SimulationNodeDatum & { id: string; hub: boolean; page: boolean; room: number }

type Tie = SimulationLinkDatum<Placed> & { path: boolean }

// How the layout holds the clusters apart and each one together, in
// design px: the room a cluster takes for so many pages, the room left
// round the middle one and between those on the ring around it, how far
// apart pages and paths sit along an edge, how hard they push each other
// away and how hard their module pulls them in.
const clusterRoom = (members: number) => 46 + 22 * Math.sqrt(members)
const middleGap = 70
const ringGap = 26
const pageLink = 52
const pathLink = 40
const pagePush = -110
const otherPush = -50
const pull = 0.3
const ticks = 600

// layout places the nodes, each at its centre, in design px (docs/webui.md
// 4.14). First the clusters: the one most tied to the others in the
// middle, at 0,0, the rest on a ring round it, each given room for its
// pages. Then each cluster's nodes round its module, drawn in tight as an
// island, empty room between the islands. A page keeps clear of others by
// its dot and its name; a module by more, for its cluster's name beside
// it. The same wiki lays out the same every time: every chance is drawn
// from a fixed seed.
export function layout(graph: WikiGraph, clustering: Clustering): Map<string, Point> {
  const at = new Map<string, Point>()
  if (clustering.clusters.length === 0) return at
  const rand = random(7)
  const degree = degrees(graph)
  const placed = new Map<string, Placed>()
  for (const node of graph.nodes) {
    const hub = clustering.of.get(node.id) === node.id
    const r = dotRadius(glyphOf(node, hub), degree.get(node.id) ?? 0)
    const name = node.page ? [...node.page.title].slice(0, nameCap).join('') : ''
    const room = hub ? 28 : node.page ? Math.max(r + 13, textWidth(name, nameSize) * 0.48 + 8) : 10
    placed.set(node.id, { id: node.id, hub, page: Boolean(node.page), room })
  }
  const across = new Map(clustering.clusters.map((cluster) => [cluster.id, 0]))
  for (const edge of graph.edges) {
    const from = clustering.of.get(edge.from) ?? generalCluster
    const to = clustering.of.get(edge.to) ?? generalCluster
    if (from === to) continue
    across.set(from, (across.get(from) ?? 0) + 1)
    across.set(to, (across.get(to) ?? 0) + 1)
  }
  const room = (cluster: Cluster) => clusterRoom(cluster.members.length)
  const middle = clustering.clusters.reduce((best, cluster) => ((across.get(cluster.id) ?? 0) > (across.get(best.id) ?? 0) ? cluster : best))
  const ring = clustering.clusters.filter((cluster) => cluster !== middle)
  const centres = new Map<string, Point>([[middle.id, { x: 0, y: 0 }]])
  if (ring.length > 0) {
    const radius = Math.max(
      room(middle) + Math.max(...ring.map(room)) + middleGap,
      ring.reduce((sum, cluster) => sum + 2 * room(cluster) + ringGap, 0) / (2 * Math.PI),
    )
    ring.forEach((cluster, i) => {
      // A screen is wider than it is tall: the ring a little wider too.
      const angle = (i / ring.length) * Math.PI * 2 - Math.PI / 2 + 0.35
      centres.set(cluster.id, { x: Math.cos(angle) * radius * 1.08, y: Math.sin(angle) * radius })
    })
  }
  for (const cluster of clustering.clusters) {
    const centre = centres.get(cluster.id) ?? { x: 0, y: 0 }
    const members = cluster.members.flatMap((id) => placed.get(id) ?? [])
    for (const node of members) {
      if (node.hub) {
        node.x = node.fx = centre.x
        node.y = node.fy = centre.y
      } else {
        node.x = centre.x + (rand() - 0.5) * 80
        node.y = centre.y + (rand() - 0.5) * 80
      }
    }
    const inside = new Set(cluster.members)
    const links: Tie[] = graph.edges
      .filter((edge) => inside.has(edge.from) && inside.has(edge.to) && edge.from !== edge.to)
      .map((edge) => ({ source: edge.from, target: edge.to, path: edge.kind === 'names' || edge.kind === 'from' }))
    forceSimulation(members)
      .randomSource(rand)
      .force(
        'link',
        forceLink<Placed, Tie>(links)
          .id((node) => node.id)
          .distance((link) => (link.path ? pathLink : pageLink))
          .strength(0.4),
      )
      .force(
        'charge',
        forceManyBody<Placed>().strength((node) => (node.page ? pagePush : otherPush)),
      )
      .force('collide', forceCollide<Placed>((node) => node.room).iterations(4))
      .force('x', forceX<Placed>(centre.x).strength(pull))
      .force('y', forceY<Placed>(centre.y).strength(pull))
      .stop()
      .tick(ticks)
    for (const node of members) at.set(node.id, { x: node.x ?? 0, y: node.y ?? 0 })
  }
  return at
}

// random is a small seeded generator, for what the layout leaves to chance.
function random(seed: number): () => number {
  let state = seed
  return () => {
    state = (state * 16807) % 2147483647
    return state / 2147483647
  }
}
