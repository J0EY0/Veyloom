import { fireEvent, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import type { WikiGraph } from '@/api/types'
import { stubApi } from '@/test/fetch'
import { renderWithProviders } from '@/test/render'
import { WikiView } from '../WikiView'
import { info, routes } from '../wikiTesting'

// The relation graph of a project's wiki (docs/design.md 5.17): what it
// draws by default, what the filters add, and what focusing on a node says.

const graph: WikiGraph = {
  nodes: [
    {
      id: '/decisions/brief.md',
      kind: 'page',
      page: info('/decisions/brief.md', 'Decision', 'Brief shape', { resident: true, description: 'One section per part.' }),
    },
    {
      id: '/pitfalls/empty.md',
      kind: 'page',
      page: info('/pitfalls/empty.md', 'Pitfall', 'Empty room', {
        review: { why: 'changed', file: 'internal/hub/brief.go', changed_at: '2026-09-20T03:00:00Z' },
      }),
    },
    { id: '/facts/old.md', kind: 'page', page: info('/facts/old.md', 'Fact', 'Old news', { status: 'deprecated' }) },
    { id: 'file:internal/hub/brief.go', kind: 'file', file: 'internal/hub/brief.go', pages: 2 },
    { id: 'topic:t3', kind: 'topic', topic: { room_id: 'r1', thread_id: 't3', number: 3, title: 'How the brief is put together' } },
  ],
  edges: [
    { from: '/pitfalls/empty.md', to: '/decisions/brief.md', kind: 'link', context: 'An empty room once broke [the brief].' },
    { from: '/decisions/brief.md', to: '/facts/old.md', kind: 'supersedes' },
    { from: '/decisions/brief.md', to: 'file:internal/hub/brief.go', kind: 'names' },
    { from: '/pitfalls/empty.md', to: 'file:internal/hub/brief.go', kind: 'names' },
    { from: '/decisions/brief.md', to: 'topic:t3', kind: 'from' },
  ],
}

function renderGraph(search = '', onOpenThread = vi.fn(), wiki: WikiGraph = graph) {
  stubApi(routes({ '/projects/p1/wiki/graph': { graph: wiki } }))
  return renderWithProviders(<WikiView space={{ kind: 'project', projectId: 'p1', roomId: 'r1' }} rest="graph" onOpenThread={onOpenThread} />, {
    route: `/rooms/r1/wiki/graph${search}`,
  })
}

const card = (name: string) => screen.getByRole('region', { name })

describe('WikiGraphView', () => {
  it('draws the pages and the paths two of them name; the filters add the rest', async () => {
    renderGraph()
    expect(await screen.findByText('Brief shape')).toBeInTheDocument()
    expect(screen.getByText('Empty room')).toBeInTheDocument()
    expect(screen.getByText('internal/hub/brief.go')).toBeInTheDocument()
    expect(screen.queryByText('Old news')).not.toBeInTheDocument()
    expect(screen.queryByText('话题 #3')).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: '关系图' })).toHaveAttribute('href', '/rooms/r1/wiki/graph')

    await userEvent.click(screen.getByRole('button', { name: '筛选' }))
    await userEvent.click(screen.getByRole('switch', { name: '已废弃的页' }))
    expect(await screen.findByText('Old news')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('switch', { name: '页面出自的话题' }))
    expect(await screen.findByText('话题 #3')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('checkbox', { name: '决定' }))
    expect(screen.queryByText('Brief shape')).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '恢复默认' }))
    expect(await screen.findByText('Brief shape')).toBeInTheDocument()
    expect(screen.queryByText('Old news')).not.toBeInTheDocument()
  })

  it('focuses on the node a link names, and says how it relates to the rest', async () => {
    const { router } = renderGraph('?focus=%2Fdecisions%2Fbrief.md')
    const brief = await screen.findByRole('region', { name: 'Brief shape' })
    expect(within(brief).getByText('决定')).toBeInTheDocument()
    expect(within(brief).getByText('/decisions/brief.md')).toBeInTheDocument()
    expect(within(brief).getByText('One section per part.')).toBeInTheDocument()
    expect(within(brief).getByText('常驻')).toBeInTheDocument()
    expect(within(brief).getByRole('link', { name: '打开页面' })).toHaveAttribute('href', '/rooms/r1/wiki/decisions/brief.md')
    const linked = within(brief).getByRole('region', { name: '引用此页的页面' })
    expect(within(linked).getByText('An empty room once broke [the brief].')).toBeInTheDocument()
    expect(within(within(brief).getByRole('region', { name: '提及的路径' })).getByText('internal/hub/brief.go')).toBeInTheDocument()
    // The deprecated page it took over from is not on screen, so not listed.
    expect(within(brief).queryByRole('region', { name: '取代了' })).not.toBeInTheDocument()

    await userEvent.click(within(linked).getByRole('button', { name: /Empty room/ }))
    expect(router.state.location.search).toBe(`?${new URLSearchParams({ focus: '/pitfalls/empty.md' })}`)
    expect(within(card('Empty room')).getByText('待复核 · internal/hub/brief.go 在这一页写成后有改动（9月20日）')).toBeInTheDocument()

    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('region', { name: 'Empty room' })).not.toBeInTheDocument()
    expect(router.state.location.search).toBe('')
  })

  it('focuses on a node picked on the canvas, or found by name', async () => {
    const { router } = renderGraph()
    // A click, without the press React Flow reads as the start of a drag.
    fireEvent.click(await screen.findByText('Empty room'))
    expect(await screen.findByRole('region', { name: 'Empty room' })).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: '查找节点' }))
    await userEvent.type(screen.getByPlaceholderText('按标题或路径查找…'), 'brief.go')
    await userEvent.click(screen.getByRole('option', { name: /internal\/hub\/brief\.go/ }))
    const file = card('internal/hub/brief.go')
    expect(within(file).getByText('仓库路径')).toBeInTheDocument()
    expect(within(file).getByText('2 页提及')).toBeInTheDocument()
    expect(within(within(file).getByRole('region', { name: '提及它的页面' })).getAllByRole('button')).toHaveLength(2)
    expect(router.state.location.search).toBe(`?${new URLSearchParams({ focus: 'file:internal/hub/brief.go' })}`)
  })

  it('shows what a link asks to focus on, a deprecated page or a topic, and opens the topic', async () => {
    const onOpenThread = vi.fn()
    const { unmount } = renderGraph('?focus=%2Ffacts%2Fold.md', onOpenThread)
    const old = await screen.findByRole('region', { name: 'Old news' })
    expect(within(old).getByText('已废弃')).toBeInTheDocument()
    expect(within(within(old).getByRole('region', { name: '被以下页面取代' })).getByText('Brief shape')).toBeInTheDocument()
    unmount()

    renderGraph('?focus=topic%3At3', onOpenThread)
    const topic = await screen.findByRole('region', { name: '话题 #3 How the brief is put together' })
    expect(within(topic).getByText('How the brief is put together')).toBeInTheDocument()
    await userEvent.click(within(topic).getByRole('button', { name: '打开话题' }))
    expect(onOpenThread).toHaveBeenCalledWith('t3')
  })

  it('says when there is nothing to draw', async () => {
    renderGraph('', vi.fn(), { nodes: [], edges: [] })
    expect(await screen.findByText('还没有页面')).toBeInTheDocument()
  })
})
