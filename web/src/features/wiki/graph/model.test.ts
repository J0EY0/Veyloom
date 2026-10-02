import { describe, expect, it } from 'vitest'
import type { GraphNode, WikiGraph, WikiPageInfo } from '@/api/types'
import { foldPaths } from './fold'
import { clusterGraph, layout } from './layout'
import { defaultFilters, filtersShowing, neighborhood, pageRelations, relationsOf, steps, visibleGraph } from './model'

function page(id: string, overrides: Partial<WikiPageInfo> = {}): GraphNode {
  return {
    id,
    kind: 'page',
    page: {
      path: id,
      type: 'Fact',
      title: id,
      tags: [],
      status: 'stable',
      tier: 'unverified',
      modified: '2026-09-23T00:00:00Z',
      resident: false,
      ...overrides,
    },
  }
}

const graph: WikiGraph = {
  nodes: [
    page('/a.md'),
    page('/b.md'),
    page('/old.md', { status: 'deprecated' }),
    { ...page('/@acme/x.md', { mount: 'acme' }), kind: 'external' },
    { id: 'file:go.mod', kind: 'file', file: 'go.mod', pages: 2 },
    { id: 'file:internal/hub/brief.go', kind: 'file', file: 'internal/hub/brief.go', pages: 2 },
    { id: 'file:only.go', kind: 'file', file: 'only.go', pages: 1 },
    { id: 'topic:t1', kind: 'topic', topic: { room_id: 'r1', thread_id: 't1', number: 1, title: 'x' } },
  ],
  edges: [
    { from: '/a.md', to: '/b.md', kind: 'link', context: 'See [b].' },
    { from: '/b.md', to: '/old.md', kind: 'supersedes' },
    { from: '/a.md', to: '/@acme/x.md', kind: 'link' },
    { from: '/a.md', to: 'file:go.mod', kind: 'names' },
    { from: '/b.md', to: 'file:go.mod', kind: 'names' },
    // Named by a page shown and a deprecated one: not shared among those shown.
    { from: '/a.md', to: 'file:internal/hub/brief.go', kind: 'names' },
    { from: '/old.md', to: 'file:internal/hub/brief.go', kind: 'names' },
    { from: '/b.md', to: 'file:only.go', kind: 'names' },
    { from: '/a.md', to: 'topic:t1', kind: 'from' },
  ],
}

const ids = (g: WikiGraph) => g.nodes.map((node) => node.id).sort()

describe('the relation graph', () => {
  it('shows by default the current pages, mounted ones, and the paths pages shown share', () => {
    const shown = visibleGraph(graph, defaultFilters)
    expect(ids(shown)).toEqual(['/@acme/x.md', '/a.md', '/b.md', 'file:go.mod'])
    expect(shown.edges.map((edge) => `${edge.kind} ${edge.from} ${edge.to}`)).toEqual([
      'link /a.md /b.md',
      'link /a.md /@acme/x.md',
      'names /a.md file:go.mod',
      'names /b.md file:go.mod',
    ])
  })

  it('shows what the filters ask for', () => {
    expect(ids(visibleGraph(graph, { ...defaultFilters, deprecated: true }))).toContain('file:internal/hub/brief.go')
    expect(ids(visibleGraph(graph, { ...defaultFilters, files: 'all' }))).toEqual(
      expect.arrayContaining(['file:only.go', 'file:internal/hub/brief.go', 'file:go.mod']),
    )
    expect(ids(visibleGraph(graph, { ...defaultFilters, files: 'none', external: false }))).toEqual(['/a.md', '/b.md'])
    expect(ids(visibleGraph(graph, { ...defaultFilters, topics: true }))).toContain('topic:t1')
    expect(visibleGraph(graph, { ...defaultFilters, hiddenKinds: ['link'] }).edges.every((edge) => edge.kind !== 'link')).toBe(true)
    expect(ids(visibleGraph(graph, { ...defaultFilters, hiddenTypes: ['Fact'] }))).toEqual([])
  })

  it('turns on what it takes to show the node a link asks to focus on', () => {
    expect(filtersShowing(graph, defaultFilters, '/old.md')).toEqual({ ...defaultFilters, deprecated: true })
    expect(filtersShowing(graph, { ...defaultFilters, hiddenTypes: ['Fact', 'Skill'] }, '/a.md').hiddenTypes).toEqual(['Skill'])
    expect(filtersShowing(graph, defaultFilters, 'file:only.go').files).toBe('all')
    expect(filtersShowing(graph, defaultFilters, 'file:go.mod')).toEqual(defaultFilters)
    expect(filtersShowing(graph, defaultFilters, 'topic:t1').topics).toBe(true)
    expect(filtersShowing(graph, defaultFilters, '/nowhere.md')).toEqual(defaultFilters)
  })

  it('groups the relations of a node by kind and direction', () => {
    const groups = relationsOf(graph, '/b.md').map((group) => [group.key, group.related.map((r) => r.node.id + (r.context ? ` ${r.context}` : ''))])
    expect(groups).toEqual([
      ['linkedFrom', ['/a.md See [b].']],
      ['supersedes', ['/old.md']],
      ['names', ['file:go.mod', 'file:only.go']],
    ])
    expect(relationsOf(graph, 'file:go.mod')).toEqual([{ key: 'namedBy', related: [{ node: graph.nodes[0] }, { node: graph.nodes[1] }] }])
  })

  it('reaches out from the node in focus, whichever way edges run', () => {
    const shown = visibleGraph(graph, { ...defaultFilters, deprecated: true })
    expect([...neighborhood(shown, '/b.md', 1)].sort()).toEqual(['/a.md', '/b.md', '/old.md', 'file:go.mod'])
    expect(neighborhood(shown, '/old.md', 2)).toContain('/a.md')
  })

  it('lists for a page its relations, and the pages sharing a path or a topic with it', () => {
    const relations = pageRelations(graph, '/a.md')
    expect(relations.direct.map((group) => [group.key, group.related.map((r) => r.node.id)])).toEqual([['linksTo', ['/@acme/x.md', '/b.md']]])
    expect(relations.files.map((group) => [group.via.id, group.pages.map((node) => node.id)])).toEqual([
      ['file:go.mod', ['/b.md']],
      ['file:internal/hub/brief.go', ['/old.md']],
    ])
    // Nothing else came from its topic.
    expect(relations.topics).toEqual([])
  })

  it('counts the steps out from the node in focus', () => {
    const shown = visibleGraph(graph, { ...defaultFilters, deprecated: true })
    expect(Object.fromEntries(steps(shown, '/old.md', 2))).toEqual({ '/old.md': 0, '/b.md': 1, 'file:internal/hub/brief.go': 1, '/a.md': 2, 'file:go.mod': 2 })
  })

  it('folds the paths pages name into their directories, each edge saying which paths', () => {
    const folded = foldPaths(graph)
    expect(folded.graph.nodes.filter((node) => node.kind === 'file')).toEqual([
      { id: 'file:./', kind: 'file', file: './', dir: true, pages: 2 },
      { id: 'file:internal/hub/', kind: 'file', file: 'internal/hub/', dir: true, pages: 2 },
    ])
    expect(folded.graph.edges.filter((edge) => edge.kind === 'names')).toEqual([
      { from: '/a.md', to: 'file:./', kind: 'names', context: 'go.mod' },
      { from: '/b.md', to: 'file:./', kind: 'names', context: 'go.mod, only.go' },
      { from: '/a.md', to: 'file:internal/hub/', kind: 'names', context: 'brief.go' },
      { from: '/old.md', to: 'file:internal/hub/', kind: 'names', context: 'brief.go' },
    ])
    expect(folded.into.get('file:only.go')).toBe('file:./')
    expect(folded.paths.get('file:./')).toEqual(['go.mod', 'only.go'])
    // A directory two pages shown name a path in shows; one only a
    // deprecated page shares does not.
    expect(ids(visibleGraph(folded.graph, defaultFilters))).toEqual(['/@acme/x.md', '/a.md', '/b.md', 'file:./'])
    expect(filtersShowing(folded.graph, defaultFilters, 'file:internal/hub/').files).toBe('all')
  })

  it('puts each page in the module it relates to, or the general cluster', () => {
    const clustering = clusterGraph(modular)
    expect(Object.fromEntries(clustering.of)).toEqual({
      '/m/store.md': '/m/store.md',
      '/m/sync.md': '/m/sync.md',
      '/f/where.md': '/m/store.md',
      // Two steps out, by way of the page it links to.
      '/d/json.md': '/m/store.md',
      // It links to a page about storage, and to the sync module itself.
      '/p/lww.md': '/m/sync.md',
      '/c/vet.md': '',
      '/c/fmt.md': '',
      // A deprecated module is a page like any other.
      '/m/old.md': '/m/store.md',
      // With the most of the pages naming a path in it.
      'file:internal/store/': '/m/store.md',
    })
    expect(clustering.clusters.map((cluster) => [cluster.id, cluster.members.length])).toEqual([
      ['/m/store.md', 5],
      ['/m/sync.md', 2],
      ['', 2],
    ])
  })

  it('lays the same graph out the same, each cluster an island round its module', () => {
    const clustering = clusterGraph(modular)
    const first = layout(modular, clustering)
    expect([...first.entries()]).toEqual([...layout(modular, clustering).entries()])
    // The cluster most tied to the others in the middle.
    expect(first.get('/m/store.md')).toEqual({ x: 0, y: 0 })
    const distance = (a: string, b: string) => Math.hypot(first.get(a)!.x - first.get(b)!.x, first.get(a)!.y - first.get(b)!.y)
    for (const id of ['/f/where.md', '/d/json.md', '/m/old.md', 'file:internal/store/'])
      expect(distance(id, '/m/store.md')).toBeLessThan(distance(id, '/m/sync.md'))
    expect(distance('/p/lww.md', '/m/sync.md')).toBeLessThan(distance('/p/lww.md', '/m/store.md'))
    // Islands, with room between them.
    expect(distance('/m/store.md', '/m/sync.md')).toBeGreaterThan(150)
  })
})

const modular: WikiGraph = {
  nodes: [
    page('/m/store.md', { type: 'Module' }),
    page('/m/sync.md', { type: 'Module' }),
    page('/f/where.md'),
    page('/d/json.md', { type: 'Decision' }),
    page('/p/lww.md', { type: 'Decision' }),
    page('/c/vet.md', { type: 'Convention' }),
    page('/c/fmt.md', { type: 'Convention' }),
    page('/m/old.md', { type: 'Module', status: 'deprecated' }),
    { id: 'file:internal/store/', kind: 'file', file: 'internal/store/', dir: true, pages: 2 },
  ],
  edges: [
    { from: '/f/where.md', to: '/m/store.md', kind: 'link' },
    { from: '/d/json.md', to: '/f/where.md', kind: 'link' },
    { from: '/p/lww.md', to: '/f/where.md', kind: 'link' },
    { from: '/p/lww.md', to: '/m/sync.md', kind: 'link' },
    { from: '/c/vet.md', to: '/c/fmt.md', kind: 'link' },
    { from: '/m/old.md', to: '/m/store.md', kind: 'supersedes' },
    { from: '/f/where.md', to: 'file:internal/store/', kind: 'names' },
    { from: '/d/json.md', to: 'file:internal/store/', kind: 'names' },
  ],
}
