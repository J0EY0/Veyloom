import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { stubApi } from '@/test/fetch'
import { renderWithProviders } from '@/test/render'
import { pickOption } from '@/test/select'
import { LibraryPage } from './LibraryPage'
import { agent, agents, catalog, market, routes, skill } from './library/libraryTesting'

// The skill library as a market (docs/webui.md 4.10): its list of skills,
// narrowed and installed from, its patterns, its menu, and importing.

describe('LibraryPage', () => {
  it('lists the skills the way a market does: whose, how they stand, whom they are installed for', async () => {
    stubApi(routes({ '/library': { wiki: market }, '/agents': { agents } }))
    renderWithProviders(<LibraryPage />, { route: '/library', path: '/library/*' })
    const list = await screen.findByRole('list', { name: '技能' })
    const rows = within(list).getAllByRole('listitem')
    expect(rows.map((row) => within(row).getByRole('link').textContent)).toEqual(['Go table tests', 'Release notes'])
    expect(within(rows[0]).getByRole('link')).toHaveAttribute('href', '/library/skills/go-table-tests/SKILL.md')
    expect(rows[0]).toHaveTextContent('试用中')
    expect(rows[0]).toHaveTextContent('Write the cases as a table.')
    expect(rows[0]).toHaveTextContent('「Veyloom」团队')
    expect(rows[0]).toHaveTextContent('装给了 Coder、Writer')
    expect(rows[1]).toHaveTextContent('只给 Codex')
    expect(rows[1]).toHaveTextContent('装给了 Coder')
    // No pages here: the files a skill came with are on its own page.
    expect(screen.queryByText('Naming cases')).toBeNull()
    expect(screen.queryByText(/页/)).toBeNull()
    // The retired one folds away at the end.
    await userEvent.click(screen.getByRole('button', { name: /已停用的技能/ }))
    expect(within(await screen.findByRole('list', { name: '已停用的技能' })).getByRole('link', { name: 'Old habit' })).toBeInTheDocument()
    // The views are skills and patterns.
    const views = screen.getByRole('navigation', { name: '技能库视图' })
    expect(within(views).getByRole('link', { name: '技能' })).toHaveAttribute('aria-current', 'page')
    expect(within(views).getByRole('link', { name: '模式' })).toHaveAttribute('href', '/library/patterns')
  })

  it('narrows by words and by team, and says when nothing matches', async () => {
    stubApi(routes({ '/library': { wiki: market }, '/agents': { agents } }))
    renderWithProviders(<LibraryPage />, { route: '/library', path: '/library/*' })
    const list = await screen.findByRole('list', { name: '技能' })
    await userEvent.type(screen.getByRole('searchbox', { name: '搜索技能' }), 'commits')
    expect(
      within(list)
        .getAllByRole('link')
        .map((link) => link.textContent),
    ).toEqual(['Release notes'])
    await userEvent.clear(screen.getByRole('searchbox', { name: '搜索技能' }))

    await userEvent.click(screen.getByRole('button', { name: '团队：全部' }))
    await userEvent.click(await screen.findByRole('menuitemradio', { name: 'Veyloom' }))
    expect(
      within(screen.getByRole('list', { name: '技能' }))
        .getAllByRole('link')
        .map((link) => link.textContent),
    ).toEqual(['Go table tests'])

    await userEvent.type(screen.getByRole('searchbox', { name: '搜索技能' }), 'zzz')
    expect(await screen.findByText('没有匹配的技能')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '清空筛选' }))
    expect(within(await screen.findByRole('list', { name: '技能' })).getAllByRole('link')).toHaveLength(2)
  })

  it('with no skill, says so in one line and offers the import, with nothing to narrow', async () => {
    stubApi(routes({ '/library': { wiki: { ...catalog, pages: [] } }, '/agents': { agents: [] } }))
    renderWithProviders(<LibraryPage />, { route: '/library', path: '/library/*' })
    const empty = (await screen.findByText('还没有技能')).closest('[data-slot="empty"]') as HTMLElement
    expect(empty.querySelector('[data-slot="empty-description"]')).toBeNull()
    expect(within(empty).getByRole('button', { name: '导入技能' })).toBeInTheDocument()
    expect(screen.queryByRole('searchbox')).toBeNull()
  })

  it('lists the patterns on their own tab, and a pattern leads back to them', async () => {
    stubApi(routes({ '/library': { wiki: market }, '/library/page': { page: { ...skill, ...market.pages[4], body: 'Fix the copies together.' } } }))
    const { router } = renderWithProviders(<LibraryPage />, { route: '/library/patterns', path: '/library/*' })
    const list = await screen.findByRole('list', { name: '模式' })
    const link = within(list).getByRole('link', { name: /Copy-pasted tests drift apart/ })
    expect(link).toHaveAttribute('href', '/library/patterns/copy-paste-tests.md')
    expect(link).toHaveTextContent('Each copy is fixed alone.')
    expect(link).toHaveTextContent('Codex 写于')
    expect(within(screen.getByRole('navigation', { name: '技能库视图' })).getByRole('link', { name: '模式' })).toHaveAttribute('aria-current', 'page')

    await userEvent.click(link)
    expect(await screen.findByRole('heading', { name: 'Copy-pasted tests drift apart' })).toBeInTheDocument()
    expect(within(screen.getByRole('navigation', { name: '技能库视图' })).getByRole('link', { name: '模式' })).toHaveAttribute('aria-current', 'page')
    await userEvent.click(within(screen.getByRole('article')).getByRole('link', { name: '模式' }))
    expect(router.state.location.pathname).toBe('/library/patterns')
  })

  it('leads the old front page and graph to the skills', async () => {
    stubApi(routes({ '/library': { wiki: { ...catalog, pages: [] } } }))
    const { router } = renderWithProviders(<LibraryPage />, { route: '/library/graph', path: '/library/*' })
    await waitFor(() => expect(router.state.location.pathname).toBe('/library'))
    expect(await screen.findByText('还没有技能')).toBeInTheDocument()
  })

  it('keeps the changes and the folder in its menu', async () => {
    stubApi(routes())
    const { router } = renderWithProviders(<LibraryPage />, { route: '/library', path: '/library/*' })
    await screen.findByRole('list', { name: '技能' })
    await userEvent.click(screen.getByRole('button', { name: '更多' }))
    expect(await screen.findByRole('menuitem', { name: '复制文件夹路径' })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('menuitem', { name: '变更记录' }))
    expect(router.state.location.pathname).toBe('/library/changes')
    expect(await screen.findByRole('heading', { name: '变更' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '技能库' })).toHaveAttribute('href', '/library')
  })

  it('installs a skill right from its row in the list', async () => {
    let asked: unknown
    stubApi(
      routes({
        '/library': { wiki: market },
        '/agents': { agents: [agent('a1', 'Coder', 'codex', ['release-notes']), agent('a4', 'Maker', 'codex'), agent('a3', 'Thinker', 'claude')] },
        '/library/install': async (req: Request) => {
          asked = await req.json()
          return {
            installed: [
              { id: 'a1', name: 'Coder' },
              { id: 'a4', name: 'Maker' },
            ],
          }
        },
      }),
    )
    renderWithProviders(<LibraryPage />, { route: '/library', path: '/library/*' })
    await userEvent.click(await screen.findByRole('button', { name: '把 Release notes 装给…' }))
    const menu = await screen.findByRole('menu')
    expect(within(menu).getByRole('menuitemcheckbox', { name: /Coder/ })).toHaveAttribute('aria-checked', 'true')
    expect(within(menu).getByRole('menuitemcheckbox', { name: /Thinker/ })).toHaveAttribute('aria-disabled', 'true')
    await userEvent.click(within(menu).getByRole('menuitemcheckbox', { name: /Maker/ }))
    await waitFor(() => expect(asked).toEqual({ name: 'release-notes', agent_id: 'a4', installed: true }))
    // The menu stays open for more; the page behind it is hidden meanwhile.
    await userEvent.keyboard('{Escape}')
    const row = (await screen.findByRole('link', { name: 'Release notes' })).closest('[role="listitem"]') as HTMLElement
    await waitFor(() => expect(row).toHaveTextContent('装给了 Coder、Maker'))
  })

  it('imports a skill from a folder and opens it', async () => {
    let asked: { folder?: string; project_id?: string } = {}
    stubApi(
      routes({
        '/library/page': { page: skill },
        '/library/usage': { uses: [] },
        '/agents': { agents: [] },
        '/library/import': async (req: Request) => {
          asked = (await req.json()) as typeof asked
          if (asked.folder === '/tmp/empty') {
            return Response.json({ error: '/tmp/empty holds no SKILL.md', code: 'skillFileMissing', params: { path: '/tmp/empty' } }, { status: 400 })
          }
          return Response.json({ page: skill }, { status: 201 })
        },
      }),
    )
    renderWithProviders(<LibraryPage />, { route: '/library', path: '/library/*' })
    await userEvent.click(await screen.findByRole('button', { name: '导入技能' }))
    const dialog = await screen.findByRole('dialog', { name: '从文件夹导入技能' })
    await userEvent.click(within(dialog).getByRole('button', { name: '导入' }))
    expect(within(dialog).getByText('要填文件夹的路径。')).toBeInTheDocument()

    await userEvent.type(within(dialog).getByLabelText('文件夹'), '/tmp/empty')
    await userEvent.click(within(dialog).getByRole('button', { name: '导入' }))
    expect(await within(dialog).findByText('/tmp/empty 里没有 SKILL.md，技能文件夹要以它为主文件。')).toBeInTheDocument()

    await userEvent.clear(within(dialog).getByLabelText('文件夹'))
    await userEvent.type(within(dialog).getByLabelText('文件夹'), '/Users/me/.claude/skills/go-table-tests')
    await pickOption('负责的团队', 'Veyloom')
    await userEvent.click(within(dialog).getByRole('button', { name: '导入' }))
    expect(await screen.findByRole('heading', { name: 'Go table tests' })).toBeInTheDocument()
    expect(asked).toEqual({ folder: '/Users/me/.claude/skills/go-table-tests', project_id: 'p1' })
    expect(screen.queryByRole('dialog')).toBeNull()
  })
})
