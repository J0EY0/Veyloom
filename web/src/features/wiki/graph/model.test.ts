import { describe, expect, it } from 'vitest'
import type { GraphNode, WikiGraph, WikiPageInfo } from '@/api/types'
import { defaultFilters, filtersShowing, layout, neighborhood, pageRelations, relationsOf, visibleGraph } from './model'

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

  it('lays the same graph out the same, nodes apart', () => {
    const shown = visibleGraph(graph, { ...defaultFilters, files: 'all', topics: true, deprecated: true })
    const first = layout(shown, () => 40)
    const again = layout(shown, () => 40)
    expect([...first.entries()]).toEqual([...again.entries()])
    const points = [...first.values()]
    for (const [i, p] of points.entries()) {
      expect(Number.isFinite(p.x) && Number.isFinite(p.y)).toBe(true)
      for (const q of points.slice(i + 1)) expect(Math.hypot(p.x - q.x, p.y - q.y)).toBeGreaterThan(40)
    }
  })
})
