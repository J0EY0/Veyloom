import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import type { WikiGraph, WikiPage } from '@/api/types'
import { librarySpace } from '@/api/wiki'
import { stubApi } from '@/test/fetch'
import { renderWithProviders } from '@/test/render'
import { WikiView } from './WikiView'
import { info, port, renderWiki, routes } from './wikiTesting'

// What a page lists of its relations (docs/design.md 5.17, step 4), and the
// way into the graph focused on it.

// sentence finds the line under a page saying where a link to it is,
// however it is split into parts.
const sentence = (text: string) => (_: string, element: Element | null) => element?.tagName === 'P' && element.textContent === text

const node = (path: string, type: string, title: string, overrides = {}) => ({ id: path, kind: 'page' as const, page: info(path, type, title, overrides) })

const graph: WikiGraph = {
  nodes: [
    node('/facts/port.md', 'Fact', 'The hub listens on 7788'),
    node('/modules/config.md', 'Module', 'Config'),
    node('/facts/old-port.md', 'Fact', 'The hub listened on 8080', { status: 'deprecated' }),
    node('/decisions/addr.md', 'Decision', 'One flag for the address'),
    node('/pitfalls/viper.md', 'Pitfall', 'Viper reads the binary'),
    { id: '/@acme/ports.md', kind: 'external', page: info('/@acme/ports.md', 'Fact', 'Company ports', { mount: 'acme' }) },
    { id: 'file:internal/config/load.go', kind: 'file', file: 'internal/config/load.go', pages: 3 },
    { id: 'topic:t3', kind: 'topic', topic: { room_id: 'r1', thread_id: 't3', number: 3, title: 'Which port' } },
  ],
  edges: [
    { from: '/facts/port.md', to: '/modules/config.md', kind: 'link', context: 'Set in [the config].' },
    { from: '/decisions/addr.md', to: '/facts/port.md', kind: 'link', context: 'The port is [the default], see [/modules/config.md].' },
    { from: '/facts/port.md', to: '/@acme/ports.md', kind: 'link', context: 'See [the company list].' },
    { from: '/facts/port.md', to: '/facts/old-port.md', kind: 'supersedes' },
    { from: '/facts/port.md', to: '/decisions/addr.md', kind: 'source', context: 'The address decision' },
    { from: '/facts/port.md', to: 'file:internal/config/load.go', kind: 'names' },
    { from: '/modules/config.md', to: 'file:internal/config/load.go', kind: 'names' },
    { from: '/pitfalls/viper.md', to: 'file:internal/config/load.go', kind: 'names' },
    { from: '/facts/port.md', to: 'topic:t3', kind: 'from' },
    { from: '/decisions/addr.md', to: 'topic:t3', kind: 'from' },
  ],
}

describe('PageRelations', () => {
  it('lists how the page relates, with the sentence each link is in, and links to the graph', async () => {
    const onOpenThread = vi.fn()
    stubApi(routes({ '/projects/p1/wiki/page': { page: port }, '/projects/p1/wiki/graph': { graph } }))
    renderWiki('facts/port.md', onOpenThread)
    const relations = await screen.findByRole('region', { name: '关系' })
    expect(await within(relations).findByRole('link', { name: /在关系图中查看/ })).toHaveAttribute(
      'href',
      `/rooms/r1/wiki/graph?${new URLSearchParams({ focus: '/facts/port.md' })}`,
    )
    const linksTo = within(relations).getByRole('region', { name: '链接到' })
    expect(within(linksTo).getByRole('link', { name: 'Config' })).toHaveAttribute('href', '/rooms/r1/wiki/modules/config.md')
    // The sentence reads without the brackets the graph writes a link with.
    expect(within(linksTo).getByText(sentence('Set in the config.'))).toBeInTheDocument()
    expect(within(linksTo).getByText(/外部 · acme/)).toBeInTheDocument()
    const linkedFrom = within(relations).getByRole('region', { name: '引用此页的页面' })
    // A link written as a page's path reads as the page's title.
    expect(within(linkedFrom).getByText(sentence('The port is the default, see Config.'))).toBeInTheDocument()
    expect(within(within(relations).getByRole('region', { name: '取代了' })).getByRole('link', { name: 'The hub listened on 8080' })).toHaveClass(
      'line-through',
    )
    // A source's title is not a sentence of the page's.
    const restsOn = within(relations).getByRole('region', { name: '依据' })
    expect(within(restsOn).queryByText('The address decision')).not.toBeInTheDocument()

    // Each page once, with the paths it shares with this one.
    const files = within(relations).getByRole('region', { name: '提及相同路径的页面' })
    expect(
      within(files)
        .getAllByRole('listitem')
        .map((item) => item.textContent),
    ).toEqual(['Config · internal/config/load.go', 'Viper reads the binary · internal/config/load.go'])
    const topics = within(relations).getByRole('region', { name: '来自同一话题的页面' })
    expect(within(topics).getByRole('listitem')).toHaveTextContent(/^One flag for the address · 话题 #3$/)
    await userEvent.click(within(topics).getByRole('button', { name: '话题 #3' }))
    expect(onOpenThread).toHaveBeenCalledWith('t3')
  })

  it('keeps a page taking over from this one out of the pages linking to it', async () => {
    const old: WikiPage = {
      ...port,
      path: '/facts/old-port.md',
      title: 'The hub listened on 8080',
      backlinks: [{ path: '/facts/port.md', title: 'The hub listens on 7788' }],
    }
    stubApi(routes({ '/projects/p1/wiki/page': { page: old }, '/projects/p1/wiki/graph': { graph } }))
    renderWiki('facts/old-port.md')
    const relations = await screen.findByRole('region', { name: '关系' })
    expect(await within(relations).findByRole('region', { name: '被以下页面取代' })).toBeInTheDocument()
    expect(within(relations).queryByRole('region', { name: '引用此页的页面' })).not.toBeInTheDocument()
  })

  it('stands in the page’s own backlinks for a page the graph does not hold', async () => {
    const inFolder: WikiPage = { ...port, path: '/facts/elsewhere.md', backlinks: [{ path: '/modules/config.md', title: 'Config' }] }
    stubApi(routes({ '/projects/p1/wiki/page': { page: inFolder }, '/projects/p1/wiki/graph': { graph } }))
    renderWiki('facts/elsewhere.md')
    const relations = await screen.findByRole('region', { name: '关系' })
    expect(within(within(relations).getByRole('region', { name: '引用此页的页面' })).getByRole('link', { name: 'Config' })).toBeInTheDocument()
    expect(within(relations).queryByRole('link', { name: /在关系图中查看/ })).not.toBeInTheDocument()
  })

  it('lists a skill’s relations without a way into a graph the library has not', async () => {
    const skill: WikiPage = { ...port, path: '/skills/tables/SKILL.md', type: 'Skill', title: 'Tables', backlinks: [] }
    const library: WikiGraph = {
      nodes: [node('/skills/tables/SKILL.md', 'Skill', 'Tables'), node('/patterns/copies.md', 'Pattern', 'Copies drift')],
      edges: [{ from: '/patterns/copies.md', to: '/skills/tables/SKILL.md', kind: 'link', context: 'Fold them into [a table].' }],
    }
    stubApi({
      '/library': { wiki: { pages: [], dirs: [], folder: '/state/wiki/library', history: true } },
      '/library/page': { page: skill },
      '/library/graph': { graph: library },
      '/library/history': { commits: [] },
      '/library/usage': { uses: [] },
      '/agents': { agents: [] },
      '/users': { users: [] },
    })
    renderWithProviders(<WikiView space={librarySpace} rest="skills/tables/SKILL.md" />, { route: '/library/skills/tables/SKILL.md' })
    const relations = await screen.findByRole('region', { name: '关系' })
    expect(await within(relations).findByText(sentence('Fold them into a table.'))).toBeInTheDocument()
    expect(within(relations).queryByRole('link', { name: /在关系图中查看/ })).not.toBeInTheDocument()
  })

  it('says a page in the graph relates to nothing yet', async () => {
    const alone: WikiGraph = { nodes: [node('/facts/port.md', 'Fact', 'The hub listens on 7788')], edges: [] }
    stubApi(routes({ '/projects/p1/wiki/page': { page: { ...port, backlinks: [] } }, '/projects/p1/wiki/graph': { graph: alone } }))
    renderWiki('facts/port.md')
    expect(await screen.findByText('还没有与其他页面关联。')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /在关系图中查看/ })).toBeInTheDocument()
  })
})
