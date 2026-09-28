import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter } from 'react-router'
import { RouterProvider } from 'react-router/dom'
import { describe, expect, it } from 'vitest'
import type { WikiProjectHit, WikiSummary } from '@/api/types'
import { SidebarProvider } from '@/components/ui/sidebar'
import { TooltipProvider } from '@/components/ui/tooltip'
import { stubApi } from '@/test/fetch'
import { project } from '@/test/fixtures'
import { catalog, info, port } from '../wikiTesting'
import { WikisPage } from './WikisPage'

// The Wiki page of the sidebar (docs/design.md 5.18): every project's wiki,
// a search through them all, and one project's wiki at the page's own
// addresses.

const wikis: WikiSummary[] = [
  { project_id: 'p2', project_name: 'Reseno', room_id: 'r2', pages: 1, due: 0 },
  { project_id: 'p1', project_name: 'Veyloom', room_id: 'r1', pages: 3, due: 1, changed_at: '2026-09-21T01:00:00Z' },
]

const hit = (projectId: string, name: string, roomId: string, path: string, title: string): WikiProjectHit => ({
  ...info(path, 'Fact', title),
  snippet: `…${title}…`,
  project_id: projectId,
  project_name: name,
  room_id: roomId,
})

function stub() {
  return stubApi({
    '/projects': { projects: [project('p1', 'Veyloom', '', 'r1'), project('p2', 'Reseno', '', 'r2')] },
    '/wikis': { wikis },
    '/wikis/search': {
      hits: [hit('p2', 'Reseno', 'r2', '/facts/dev-port.md', 'Dev port 5173'), hit('p1', 'Veyloom', 'r1', '/facts/port.md', 'Port 7788')],
    },
    '/projects/p1/wiki': { wiki: catalog },
    '/projects/p1/wiki/history': { commits: [] },
    '/projects/p1/wiki/page': { page: port },
    '/projects/p1/wiki/maintainer': { maintainer: { trigger: 'daily', idle_minutes: 30, pending: {} } },
    '/rooms/r1/members': { members: [] },
    '/users': { users: [] },
  })
}

// renderWikis renders the page at route, with the other routes it leads
// to; its router, and what it rendered, to look inside it alone.
function renderWikis(route: string) {
  const page = (
    <TooltipProvider>
      <SidebarProvider>
        <WikisPage />
      </SidebarProvider>
    </TooltipProvider>
  )
  const router = createMemoryRouter(
    [
      { path: '/wiki', element: page },
      { path: '/wiki/:projectId/*', element: page },
      { path: '/rooms/:roomId', element: <p>the chat</p> },
    ],
    { initialEntries: [route] },
  )
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const view = render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
  return { router, view }
}

describe('WikisPage', () => {
  it('lists every project’s wiki, and goes into one', async () => {
    stub()
    const { router } = renderWikis('/wiki')
    const line = await screen.findByRole('link', { name: /Veyloom.*3 页 · 1 页待复核 · 最近变更于/ })
    expect(line).toHaveAttribute('href', '/wiki/p1')
    expect(screen.getByRole('combobox')).toHaveTextContent('全部项目')
    // The left column lists them by name, with their pages.
    const projects = within(screen.getByLabelText('项目', { selector: '[data-slot="sidebar"]' }))
    expect(projects.getAllByRole('link').map((link) => link.textContent)).toEqual(['Reseno1', 'Veyloom3'])
    await userEvent.click(line)
    expect(router.state.location.pathname).toBe('/wiki/p1')
    await waitFor(() => expect(screen.getByRole('combobox')).toHaveTextContent('Veyloom'))
  })

  it('searches every project’s wiki, the hits under their projects', async () => {
    const calls = stub()
    renderWikis('/wiki')
    await userEvent.type(await screen.findByRole('searchbox', { name: '搜索全部项目的 wiki' }), 'port')
    const reseno = await screen.findByRole('list', { name: 'Reseno' })
    expect(within(reseno).getByRole('link', { name: /Dev port 5173/ })).toHaveAttribute('href', '/wiki/p2/facts/dev-port.md')
    expect(within(screen.getByRole('list', { name: 'Veyloom' })).getByRole('link', { name: /Port 7788/ })).toHaveAttribute('href', '/wiki/p1/facts/port.md')
    expect(calls.some((call) => call.startsWith('GET /wikis/search?q=port'))).toBe(true)
  })

  it('reads one project’s wiki at the page’s own addresses, its topics in their chat', async () => {
    stub()
    const { router } = renderWikis('/wiki/p1/facts/port.md')
    expect(await screen.findByRole('heading', { name: 'The hub listens on 7788' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'the config' })).toHaveAttribute('href', '/wiki/p1/modules/config.md')
    expect(screen.getByRole('link', { name: '关系图' })).toHaveAttribute('href', '/wiki/p1/graph')
    expect(screen.getByRole('link', { name: '项目记忆' })).toHaveAttribute('href', '/wiki/p1/memory')
    // No chat beside it: a topic opens in its own.
    await userEvent.click(screen.getByRole('button', { name: '话题 #3 里的一轮' }))
    expect(router.state.location.pathname).toBe('/rooms/r1')
    expect(router.state.location.search).toBe('?thread=t3')
  })

  it('opens again at the project chosen last, until every project’s is chosen', async () => {
    stub()
    renderWikis('/wiki/p1')
    await waitFor(() => expect(screen.getByRole('combobox')).toHaveTextContent('Veyloom'))
    const { router, view } = renderWikis('/wiki')
    await waitFor(() => expect(router.state.location.pathname).toBe('/wiki/p1'))
    // The router moves first and the page follows as a transition: wait for
    // the project's page itself, which remembers it on showing. Its picker,
    // not the first page's, still open beside it.
    const picker = () => within(view.container).getByRole('combobox')
    await waitFor(() => expect(picker()).toHaveTextContent('Veyloom'))

    await userEvent.click(picker())
    await userEvent.click(await screen.findByRole('option', { name: '全部项目' }))
    // The router moves on after the click has returned.
    await waitFor(() => expect(router.state.location.pathname).toBe('/wiki'))
    expect(localStorage.getItem('veyloom.wikiProject')).toBeNull()
  })

  it('with no project, says so in one line, and has nothing to search', async () => {
    stubApi({ '/projects': { projects: [] }, '/wikis': { wikis: [] } })
    renderWikis('/wiki')
    const empty = (await screen.findByText('还没有项目')).closest('[data-slot="empty"]')
    expect(empty?.querySelector('[data-slot="empty-description"]')).toBeNull()
    expect(screen.queryByRole('searchbox')).not.toBeInTheDocument()
  })

  it('says when the project is not there', async () => {
    stub()
    renderWikis('/wiki/gone')
    expect(await screen.findByText('没有这个项目')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '全部项目' })).toHaveAttribute('href', '/wiki')
  })
})
