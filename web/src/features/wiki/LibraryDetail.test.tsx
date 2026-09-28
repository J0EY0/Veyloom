import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { setLocale } from '@/lib/i18n'
import { stubApi } from '@/test/fetch'
import { renderWithProviders } from '@/test/render'
import { LibraryPage } from './LibraryPage'
import { agent, market, routes, skill } from './library/libraryTesting'

// A page of the skill library shown alone (docs/webui.md 4.10): a skill,
// whose it is and whom it is installed for, how its trial goes, and the
// files it came with.

describe('a skill of the library', () => {
  it('shows a skill with its team, which a person hands on, the files it came with, and the turns that used it', async () => {
    let handed: unknown
    stubApi(
      routes({
        '/library': { wiki: market },
        '/library/page': { page: skill },
        '/library/usage': {
          uses: [
            {
              turn_id: 'x1',
              status: 'done',
              runtime: 'claude',
              started_at: '2026-09-21T02:00:00Z',
              room_id: 'r2',
              thread_id: 't7',
              topic_number: 7,
              member_name: 'Writer',
              project_id: 'p2',
              project_name: 'Docs site',
            },
          ],
        },
        '/library/transfer': async (req: Request) => {
          handed = await req.json()
          return { page: { ...skill, team: 'docs-site' } }
        },
      }),
    )
    renderWithProviders(<LibraryPage />, { route: '/library/skills/go-table-tests/SKILL.md', path: '/library/*' })
    expect(await screen.findByRole('heading', { name: 'Go table tests' })).toBeInTheDocument()
    // Back to the list, not the kind and path of a page.
    expect(screen.getByRole('link', { name: '技能库' })).toHaveAttribute('href', '/library')
    expect(screen.queryByText('/skills/go-table-tests/SKILL.md')).toBeNull()
    expect(screen.getByText('由「Veyloom」团队负责')).toBeInTheDocument()
    // No resident pages in the library: its skills go to runtimes.
    expect(screen.queryByRole('button', { name: '设为常驻' })).toBeNull()
    const files = await screen.findByRole('region', { name: '附带的文件' })
    expect(within(files).getByRole('link', { name: 'Naming cases' })).toHaveAttribute('href', '/library/skills/go-table-tests/references/naming.md')
    expect(files).toHaveTextContent('references/naming.md')
    const use = await screen.findByRole('link', { name: 'Docs site · 话题 #7' })
    expect(use).toHaveAttribute('href', '/rooms/r2?thread=t7')
    expect(screen.getByText('Writer · Claude Code')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Copy-pasted tests' })).toHaveAttribute('href', '/library/patterns/copy-paste-tests.md')

    await userEvent.click(screen.getByRole('button', { name: /转给/ }))
    await userEvent.click(await screen.findByRole('menuitem', { name: 'Docs site' }))
    await waitFor(() => expect(handed).toEqual({ name: 'go-table-tests', project_id: 'p2' }))
  })

  it('leads a file a skill came with back to the skill', async () => {
    stubApi(routes({ '/library': { wiki: market }, '/library/page': { page: { ...skill, ...market.pages[1], body: 'Name them.' } } }))
    renderWithProviders(<LibraryPage />, { route: '/library/skills/go-table-tests/references/naming.md', path: '/library/*' })
    expect(await screen.findByRole('heading', { name: 'Naming cases' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Go table tests' })).toHaveAttribute('href', '/library/skills/go-table-tests/SKILL.md')
  })

  it('says when a skill is not there', async () => {
    stubApi(routes({ '/library/page': () => Response.json({ error: 'no page' }, { status: 404 }) }))
    renderWithProviders(<LibraryPage />, { route: '/library/skills/gone/SKILL.md', path: '/library/*' })
    expect(await screen.findByText('没有这个技能')).toBeInTheDocument()
    expect(screen.getAllByRole('link', { name: '技能库' }).at(-1)).toHaveAttribute('href', '/library')
  })
  it('installs a skill for agents whose runtime it is for', async () => {
    let asked: unknown
    const kept = { ...skill, tags: ['runtime-codex'], installed: [{ id: 'a1', name: 'Coder' }] }
    stubApi(
      routes({
        '/library/page': { page: kept },
        '/library/usage': { uses: [] },
        '/agents': { agents: [agent('a1', 'Coder', 'codex', ['go-table-tests']), agent('a2', 'Writer', 'codex'), agent('a3', 'Thinker', 'claude')] },
        '/library/install': async (req: Request) => {
          asked = await req.json()
          return {
            installed: [
              { id: 'a1', name: 'Coder' },
              { id: 'a2', name: 'Writer' },
            ],
          }
        },
      }),
    )
    renderWithProviders(<LibraryPage />, { route: '/library/skills/go-table-tests/SKILL.md', path: '/library/*' })
    expect(await screen.findByText('装给了 Coder')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /装给…/ }))
    const menu = await screen.findByRole('menu')
    expect(within(menu).getByRole('menuitemcheckbox', { name: /Coder/ })).toHaveAttribute('aria-checked', 'true')
    // Kept for Codex, it is not for a Claude Code agent.
    const thinker = within(menu).getByRole('menuitemcheckbox', { name: /Thinker/ })
    expect(thinker).toHaveAttribute('aria-disabled', 'true')
    expect(thinker).toHaveTextContent('只给 Codex 用')
    await userEvent.click(within(menu).getByRole('menuitemcheckbox', { name: /Writer/ }))
    await waitFor(() => expect(asked).toEqual({ name: 'go-table-tests', agent_id: 'a2', installed: true }))
    expect(await screen.findByText('装给了 Coder、Writer')).toBeInTheDocument()
  })

  it('lists whom it is installed for as the language writes a list', async () => {
    setLocale('en')
    const both = { ...skill, installed: [{ id: 'a1', name: 'Coder' }, { id: 'a2', name: 'Tester' }] }
    stubApi(
      routes({
        '/library/page': { page: both },
        '/library/usage': { uses: [] },
        '/agents': { agents: [agent('a1', 'Coder', 'codex', ['go-table-tests']), agent('a2', 'Tester', 'pi', ['go-table-tests'])] },
      }),
    )
    renderWithProviders(<LibraryPage />, { route: '/library/skills/go-table-tests/SKILL.md', path: '/library/*' })
    expect(await screen.findByText('Installed for Coder, Tester')).toBeInTheDocument()
  })
})

describe('a skill on trial', () => {
  const trial = {
    id: 'tr1',
    skill: 'go-table-tests',
    base_sha: 'abc1234',
    started_at: '2026-09-22T01:00:00Z',
    changed_at: '2026-09-22T02:00:00Z',
    turn_id: 'x4',
    changed_by: 'Coder',
    project_name: 'Veyloom',
    changes: 2,
    status: 'open' as const,
    uses: 2,
    failed: 1,
    needed: 3,
    room_id: 'r1',
    thread_id: 't4',
    topic_number: 4,
  }

  it('says how the trial goes, and is kept or rolled back by a person', async () => {
    const asked: Record<string, unknown> = {}
    stubApi(
      routes({
        '/library/page': { page: { ...skill, trial } },
        '/library/usage': { uses: [] },
        '/agents': { agents: [] },
        '/library/verify': async (req: Request) => {
          asked.verify = await req.json()
          return { page: { ...skill, trial: { ...trial, status: 'kept', ended_by: 'human:alice', ended_at: '2026-09-22T03:00:00Z' } } }
        },
        '/library/rollback': async (req: Request) => {
          asked.rollback = await req.json()
          return { page: skill }
        },
      }),
    )
    renderWithProviders(<LibraryPage />, { route: '/library/skills/go-table-tests/SKILL.md', path: '/library/*' })
    const box = await screen.findByRole('region', { name: '试用' })
    expect(box).toHaveTextContent('试用中 · 已用 2/3 轮')
    expect(box).toHaveTextContent('1 轮失败')
    expect(box).toHaveTextContent('Coder 改的')
    expect(box).toHaveTextContent('这次试用共改了 2 次')
    expect(box).toHaveTextContent('再有 1 轮顺利用上就自动转正')
    expect(within(box).getByRole('link', { name: 'Veyloom · 话题 #4' })).toHaveAttribute('href', '/rooms/r1?thread=t4')
    // Keeping it is the trial's own button, not the page's confirm.
    expect(screen.queryByRole('button', { name: '确认' })).toBeNull()

    await userEvent.click(within(box).getByRole('button', { name: '退回…' }))
    const dialog = await screen.findByRole('alertdialog', { name: '退回这次改动？' })
    await userEvent.type(within(dialog).getByLabelText('原因（可不填）'), '说明变长了')
    await userEvent.click(within(dialog).getByRole('button', { name: '退回' }))
    await waitFor(() => expect(asked.rollback).toEqual({ name: 'go-table-tests', reason: '说明变长了' }))

    await userEvent.click(within(box).getByRole('button', { name: '转正' }))
    await waitFor(() => expect(asked.verify).toEqual({ path: '/skills/go-table-tests/SKILL.md', verify: true }))
  })

  it('says how the last trial ended', async () => {
    const ended = [
      [{ status: 'kept', ended_by: 'process:skill-trial' }, '上次改动试用期满，已自动转正'],
      [{ status: 'kept', ended_by: 'human:alice', reason: 'changed by hand' }, '上次改动试用中被 alice 手动改过，已转正'],
      [{ status: 'rolled_back', ended_by: 'claude/haiku', reason: '说明变长了' }, '上次改动已被 Claude Code · haiku 退回：说明变长了'],
    ] as const
    for (const [how, words] of ended) {
      stubApi(
        routes({
          '/library/page': { page: { ...skill, trial: { ...trial, ...how, ended_at: '2026-09-22T03:00:00Z' } } },
          '/library/usage': { uses: [] },
          '/agents': { agents: [] },
        }),
      )
      const { unmount } = renderWithProviders(<LibraryPage />, { route: '/library/skills/go-table-tests/SKILL.md', path: '/library/*' })
      expect(await screen.findByText(words)).toBeInTheDocument()
      expect(screen.queryByRole('region', { name: '试用' })).toBeNull()
      unmount()
    }
  })
})
