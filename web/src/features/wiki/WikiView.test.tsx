import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { stubApi } from '@/test/fetch'
import { registerComposer } from '@/lib/composer'
import type { WikiCatalog, WikiPage } from '@/api/types'
import { catalog, commit, info, pages, port, renderWiki, routes } from './wikiTesting'

describe('WikiView', () => {
  it('lists the pages under their types, and the latest changes', async () => {
    stubApi(routes())
    renderWiki('')
    const list = await screen.findByLabelText('页面')
    await within(list).findByText('命名规范')
    // The wiki's order of types, then the rest; deprecated pages fold away.
    const labels = [...list.querySelectorAll('[data-slot="sidebar-group-label"]')].map((node) => node.textContent)
    expect(labels).toEqual(['约定1', '事实1', 'Note1', '已废弃1'])
    expect(within(list).getByText('命名规范').closest('a')).toHaveTextContent('常驻')
    expect(within(list).queryByText('Old news')).toBeNull()
    expect(within(list).getByRole('link', { name: /变更/ })).toHaveAccessibleName('变更')
    // The project memory has a place of its own, not a group of pages.
    expect(within(list).getByRole('link', { name: '项目记忆' })).toHaveAttribute('href', '/rooms/r1/wiki/memory')
    expect(within(list).queryByText('Project memory')).toBeNull()

    expect(await screen.findByRole('heading', { name: '项目 Wiki' })).toBeInTheDocument()
    expect(screen.getByText('3 页')).toBeInTheDocument()
    expect(await screen.findByText('Writer')).toBeInTheDocument()
    // What changed, at a glance: undoing is on the changes page.
    expect(within(screen.getByRole('region', { name: '最近的变更' })).queryByRole('button', { name: '撤回' })).toBeNull()
  })

  it('says what a change that touched no page did, not nothing', async () => {
    const setUp = {
      ...commit,
      sha: 'a0',
      author: 'process:veyloom',
      member: undefined,
      thread_id: undefined,
      topic_number: undefined,
      subject: 'Set up the bundle',
      changes: [],
      undoable: false,
    }
    const bare = { ...setUp, sha: 'a1', subject: 'Something of its own' }
    stubApi(routes({ '/projects/p1/wiki/history': { commits: [commit, bare, setUp] } }))
    renderWiki('changes')
    expect(await screen.findByText('建立了 wiki')).toBeInTheDocument()
    expect(screen.getByText('没有改动页面')).toBeInTheDocument()
    // Undoing is here, on the changes page.
    expect(screen.getByRole('button', { name: '撤回' })).toBeInTheDocument()
  })

  it('lists the conventions of the wiki in a place of their own, when it has them', async () => {
    const withConventions: WikiCatalog = { ...catalog, pages: [...catalog.pages, info('/conventions/wiki.md', 'Convention', 'Wiki 约定')] }
    stubApi(routes({ '/projects/p1/wiki': { wiki: withConventions } }))
    renderWiki('')
    const list = await screen.findByLabelText('页面')
    await within(list).findByText('命名规范')
    const link = within(list).getByRole('link', { name: 'Wiki 约定' })
    expect(link).toHaveAttribute('href', '/rooms/r1/wiki/conventions/wiki.md')
    // Not again among the conventions: the group counts one page still.
    const labels = [...list.querySelectorAll('[data-slot="sidebar-group-label"]')].map((node) => node.textContent)
    expect(labels).toContain('约定1')
    expect(within(list).getAllByText('Wiki 约定')).toHaveLength(1)
  })

  it('searches the pages by their text', async () => {
    const calls = stubApi(routes({ '/projects/p1/wiki/search': { hits: [{ ...pages[0], snippet: '…listens on 7788…' }] } }))
    renderWiki('')
    await userEvent.type(await screen.findByRole('searchbox', { name: '搜索 wiki' }), '7788')
    // The words around the match, what was searched for marked in them.
    const found = await screen.findByText('7788', { selector: 'mark' })
    expect(found.parentElement).toHaveTextContent(/^…listens on 7788…$/)
    expect(calls.some((call) => call.startsWith('GET /projects/p1/wiki/search?q=7788'))).toBe(true)
  })

  it('reads a page: its trust, its links, where it came from', async () => {
    const onOpenThread = vi.fn()
    stubApi(routes({ '/projects/p1/wiki/page': { page: port } }))
    renderWiki('facts/port.md', onOpenThread)
    expect(await screen.findByRole('heading', { name: 'The hub listens on 7788' })).toBeInTheDocument()
    expect(screen.getByText('未核验')).toBeInTheDocument()
    expect(screen.getByText(/Codex 写于/)).toBeInTheDocument()
    expect(screen.getByText(/尚未有人确认/)).toBeInTheDocument()
    // Links between pages stay in the wiki; others open outside.
    expect(screen.getByRole('link', { name: 'the config' })).toHaveAttribute('href', '/rooms/r1/wiki/modules/config.md')
    expect(screen.getByRole('link', { name: 'the docs' })).toHaveAttribute('target', '_blank')
    expect(screen.getByRole('link', { name: 'Config' })).toHaveAttribute('href', '/rooms/r1/wiki/modules/config.md')
    await userEvent.click(screen.getByRole('button', { name: '话题 #3 中的一轮' }))
    expect(onOpenThread).toHaveBeenCalledWith('t3')
  })

  it('ends the text with a line, then how the page relates, its details with where it came from, and its history', async () => {
    // The topic a turn of it wrote the page is there as well.
    const sources = [{ id: 'topic', resource: 'veyloom://threads/t3', room_id: 'r1', thread_id: 't3', topic_number: 3 }, ...port.sources]
    stubApi(routes({ '/projects/p1/wiki/page': { page: { ...port, sources, generated_by: 'claude/sonnet' } } }))
    renderWiki('facts/port.md')
    const details = await screen.findByRole('region', { name: '页面信息' })
    const article = details.closest('article') as HTMLElement
    // The parts after the line, in order.
    const separator = within(article).getByRole('none', { hidden: true })
    expect(separator).toHaveAttribute('data-slot', 'separator')
    const after = [...article.children].slice([...article.children].indexOf(separator) + 1)
    expect(after.map((part) => part.getAttribute('aria-label') ?? part.querySelector('h2, button')?.textContent)).toEqual(['关系', '页面信息', '改动记录'])
    // Where it came from is among its details, each place once: the turn
    // stands for its topic.
    expect(within(details).getByRole('button', { name: '话题 #3 中的一轮' })).toBeInTheDocument()
    expect(within(details).queryByRole('button', { name: '话题 #3' })).not.toBeInTheDocument()
    expect(within(details).getByRole('link', { name: 'goose' })).toHaveAttribute('href', 'https://pressly.github.io/goose/')
    expect(screen.queryByRole('region', { name: '来源' })).not.toBeInTheDocument()
    // The byline names who wrote it and when; the details add the model.
    expect(screen.getByText(/Claude Code 写于/)).toBeInTheDocument()
    expect(within(details).getByText('模型')).toBeInTheDocument()
    expect(within(details).getByText('sonnet')).toBeInTheDocument()
    expect(within(details).queryByText(/Claude Code/)).toBeNull()
  })

  it('puts a question about a page to whoever keeps the wiki, in the wiki topic', async () => {
    let asked = false
    stubApi(
      routes({
        '/projects/p1/wiki/page': { page: port },
        '/projects/p1/wiki/question': () => {
          asked = true
          return { question: { thread_id: 't9', member_id: 'm1', member_name: 'Claude' } }
        },
      }),
    )
    const onOpenThread = vi.fn()
    renderWiki('facts/port.md', onOpenThread)
    await userEvent.click(await screen.findByRole('button', { name: '有疑问…' }))
    await waitFor(() => expect(onOpenThread).toHaveBeenCalledWith('t9'))
    expect(asked).toBe(true)
    // The topic's box, once it opens, starts with them and the page.
    const box = vi.fn()
    registerComposer('t9', box)()
    expect(box).toHaveBeenCalledWith('@Claude 对 wiki 页「The hub listens on 7788」（/facts/port.md）有疑问：')
  })

  it('lets a person confirm a page and make it resident', async () => {
    const posted: { path: string; body: unknown }[] = []
    // The page as the hub has it, which each change moves on.
    let now = port
    stubApi(
      routes({
        '/projects/p1/wiki/page': () => ({ page: now }),
        '/projects/p1/wiki/verify': async (req: Request) => {
          posted.push({ path: 'verify', body: await req.json() })
          now = { ...now, tier: 'human-reviewed', vouched_at: '2026-09-21T02:00:00Z', verified: [{ by: 'human:alice', at: '2026-09-21T02:00:00Z' }] }
          return { page: now }
        },
        '/projects/p1/wiki/resident': async (req: Request) => {
          posted.push({ path: 'resident', body: await req.json() })
          now = { ...now, resident: true, tags: ['resident'] }
          return { page: now }
        },
      }),
    )
    renderWiki('facts/port.md')
    await userEvent.click(await screen.findByRole('button', { name: '确认' }))
    await waitFor(() => expect(screen.getByText(/alice 确认于/)).toBeInTheDocument())
    // Making it resident is in the page's menu.
    await userEvent.click(screen.getByRole('button', { name: '更多' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: '设为常驻' }))
    await waitFor(() =>
      expect(posted).toEqual([
        { path: 'verify', body: { path: '/facts/port.md', verify: true } },
        { path: 'resident', body: { path: '/facts/port.md', resident: true } },
      ]),
    )
  })

  it('shows the bundles the project mounts, read-only', async () => {
    const margin = info('/@acme-retail/metrics/gross-margin.md', 'Metric', 'Gross margin', { mount: 'acme-retail', tier: 'human-reviewed' })
    const withMounts: WikiCatalog = {
      ...catalog,
      mounts: [
        { name: 'acme-retail', folder: '/data/acme_retail', pages: [margin] },
        { name: 'gone', folder: '/data/gone', pages: [], error: '/data/gone is not a folder on this machine' },
      ],
    }
    const page: WikiPage = {
      ...margin,
      body: 'See [the legacy one](/@acme-retail/metrics/gross-margin-legacy.md).',
      hash: 'h9',
      file: '/data/acme_retail/metrics/gross-margin.md',
      verified: [],
      sources: [],
      backlinks: [],
    }
    const calls = stubApi(routes({ '/projects/p1/wiki': { wiki: withMounts }, '/projects/p1/wiki/page': { page } }))
    renderWiki('')
    const list = await screen.findByLabelText('页面')
    await userEvent.click(await within(list).findByText('外部 · acme-retail'))
    expect(within(list).getByRole('link', { name: 'Gross margin' })).toHaveAttribute('href', '/rooms/r1/wiki/@acme-retail/metrics/gross-margin.md')
    await userEvent.click(within(list).getByText('外部 · gone'))
    expect(within(list).getByText(/无法打开：\/data\/gone/)).toBeInTheDocument()
    // The front page lists them.
    expect(await screen.findByRole('heading', { name: '外部 wiki' })).toBeInTheDocument()
    expect(screen.getByText('1 页')).toBeInTheDocument()
    expect(calls.some((call) => call.startsWith('GET /projects/p1/wiki/page'))).toBe(false)
  })

  it('reads a mounted page without the ways to change it', async () => {
    const margin = info('/@acme-retail/metrics/gross-margin.md', 'Metric', 'Gross margin', { mount: 'acme-retail' })
    const page: WikiPage = {
      ...margin,
      body: 'Revenue less cost.',
      hash: 'h9',
      file: '/data/acme_retail/metrics/gross-margin.md',
      verified: [],
      sources: [],
      backlinks: [],
    }
    const calls = stubApi(routes({ '/projects/p1/wiki/page': { page } }))
    renderWiki('@acme-retail/metrics/gross-margin.md')
    expect(await screen.findByRole('heading', { name: 'Gross margin' })).toBeInTheDocument()
    expect(screen.getByText('外部 · acme-retail · 只读')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '确认' })).toBeNull()
    expect(screen.queryByRole('button', { name: /常驻/ })).toBeNull()
    expect(screen.queryByText('改动记录')).toBeNull()
    expect(calls).toContain('GET /projects/p1/wiki/page?path=%2F%40acme-retail%2Fmetrics%2Fgross-margin.md')
  })

  it('says so when a page is not there', async () => {
    stubApi(routes({ '/projects/p1/wiki/page': Response.json({ error: 'no page' }, { status: 404 }) }))
    renderWiki('facts/gone.md')
    expect(await screen.findByText('页面不存在')).toBeInTheDocument()
  })

  it('undoes a change after asking', async () => {
    let reverted: unknown
    stubApi(
      routes({
        '/projects/p1/wiki/revert': async (req: Request) => {
          reverted = await req.json()
          return { commit: 'def' }
        },
      }),
    )
    renderWiki('changes')
    expect(await screen.findByRole('heading', { name: '变更' })).toBeInTheDocument()
    await userEvent.click(await screen.findByRole('button', { name: '撤回' }))
    const dialog = await screen.findByRole('alertdialog', { name: '撤回这次改动？' })
    // Why, for the log, so whoever keeps the wiki does not do it again.
    await userEvent.type(within(dialog).getByLabelText(/撤回原因/), ' 说反了 ')
    await userEvent.click(within(dialog).getByRole('button', { name: '撤回' }))
    await waitFor(() => expect(reverted).toEqual({ sha: 'abc', reason: '说反了' }))
  })
})
