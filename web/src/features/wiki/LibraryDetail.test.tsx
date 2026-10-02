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
    // What it is and how it stands, above its text, in one block; under the
    // text, only the files it came with.
    const facts = screen.getByText('负责团队').closest('dl') as HTMLElement
    expect(within(facts).getByText('Veyloom')).toBeInTheDocument()
    expect(within(facts).getByRole('button', { name: '改动记录 · 查看' })).toBeInTheDocument()
    // No resident pages in the library: its skills go to runtimes.
    expect(screen.queryByRole('button', { name: '设为常驻' })).toBeNull()
    const files = await screen.findByRole('region', { name: '附带的文件' })
    expect(within(files).getByRole('link', { name: 'Naming cases' })).toHaveAttribute('href', '/library/skills/go-table-tests/references/naming.md')
    expect(files).toHaveTextContent('references/naming.md')
    const use = await screen.findByRole('link', { name: /^Docs site · 话题 #7/ })
    expect(use).toHaveAttribute('href', '/rooms/r2?thread=t7')
    // A use says who ran it, the runtime in the tint of the face.
    expect(within(facts).getByRole('link', { name: /^Docs site · 话题 #7/ })).toHaveTextContent('Writer')
    expect(facts.compareDocumentPosition(files) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Copy-pasted tests' })).toHaveAttribute('href', '/library/patterns/copy-paste-tests.md')

    await userEvent.click(screen.getByRole('button', { name: /转交/ }))
    await userEvent.click(await screen.findByRole('menuitem', { name: 'Docs site' }))
    await waitFor(() => expect(handed).toEqual({ name: 'go-table-tests', project_id: 'p2' }))
  })

  it('leads a file a skill came with back to the skill', async () => {
    stubApi(routes({ '/library': { wiki: market }, '/library/page': { page: { ...skill, ...market.pages[1], body: 'Name them.' } } }))
    renderWithProviders(<LibraryPage />, { route: '/library/skills/go-table-tests/references/naming.md', path: '/library/*' })
    expect(await screen.findByRole('heading', { name: 'Naming cases' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Go table tests' })).toHaveAttribute('href', '/library/skills/go-table-tests/SKILL.md')
  })

  it('downloads a skill as a zip of its folder, a file it came with not', async () => {
    stubApi(routes({ '/library/page': { page: skill }, '/library/usage': { uses: [] }, '/agents': { agents: [] } }))
    const { unmount } = renderWithProviders(<LibraryPage />, { route: '/library/skills/go-table-tests/SKILL.md', path: '/library/*' })
    // The page's own menu, not the library's.
    await userEvent.click(within(await screen.findByRole('article')).getByRole('button', { name: '更多' }))
    const download = await screen.findByRole('menuitem', { name: '下载技能（.zip）' })
    expect(download).toHaveAttribute('href', '/api/v1/library/export?name=go-table-tests')
    expect(download).toHaveAttribute('download', 'go-table-tests.zip')
    unmount()

    stubApi(routes({ '/library': { wiki: market }, '/library/page': { page: { ...skill, ...market.pages[1], body: 'Name them.' } } }))
    renderWithProviders(<LibraryPage />, { route: '/library/skills/go-table-tests/references/naming.md', path: '/library/*' })
    await userEvent.click(within(await screen.findByRole('article')).getByRole('button', { name: '更多' }))
    expect(await screen.findByRole('menuitem', { name: '复制链接' })).toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '下载技能（.zip）' })).toBeNull()
  })

  it('says when a skill is not there', async () => {
    stubApi(routes({ '/library/page': () => Response.json({ error: 'no page' }, { status: 404 }) }))
    renderWithProviders(<LibraryPage />, { route: '/library/skills/gone/SKILL.md', path: '/library/*' })
    expect(await screen.findByText('技能不存在')).toBeInTheDocument()
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
    expect(await screen.findByText('Coder', { selector: 'dd span' })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /管理…/ }))
    const menu = await screen.findByRole('menu')
    expect(within(menu).getByRole('menuitemcheckbox', { name: /Coder/ })).toHaveAttribute('aria-checked', 'true')
    // Kept for Codex, it is not for a Claude Code agent: greyed, its runtime
    // under its name as for the others, and nothing more said.
    const thinker = within(menu).getByRole('menuitemcheckbox', { name: /Thinker/ })
    expect(thinker).toHaveAttribute('aria-disabled', 'true')
    expect(thinker).toHaveTextContent('ThinkerClaude Code')
    await userEvent.click(within(menu).getByRole('menuitemcheckbox', { name: /Writer/ }))
    await waitFor(() => expect(asked).toEqual({ name: 'go-table-tests', agent_id: 'a2', installed: true }))
    expect(await screen.findByText('Writer', { selector: 'dd span' })).toBeInTheDocument()
  })

  it('shows each agent it is installed for as a chip of its own', async () => {
    setLocale('en')
    const both = {
      ...skill,
      installed: [
        { id: 'a1', name: 'Coder' },
        { id: 'a2', name: 'Tester' },
      ],
    }
    stubApi(
      routes({
        '/library/page': { page: both },
        '/library/usage': { uses: [] },
        '/agents': { agents: [agent('a1', 'Coder', 'codex', ['go-table-tests']), agent('a2', 'Tester', 'pi', ['go-table-tests'])] },
      }),
    )
    renderWithProviders(<LibraryPage />, { route: '/library/skills/go-table-tests/SKILL.md', path: '/library/*' })
    const facts = (await screen.findByText('Installed for')).closest('dl') as HTMLElement
    expect(within(facts).getByText('Coder')).toBeInTheDocument()
    expect(within(facts).getByText('Tester')).toBeInTheDocument()
    expect(within(facts).queryByText('Coder, Tester')).toBeNull()
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

  it('says beside the title how the trial goes, and is kept or rolled back by a person', async () => {
    const asked: Record<string, unknown> = {}
    stubApi(
      routes({
        '/library/page': { page: { ...skill, generated_by: 'pi/deepseek', generated_at: '2026-09-22T02:00:00Z', trial } },
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
    const box = await screen.findByRole('group', { name: '试用' })
    expect(box).toHaveTextContent('试用中 · 已用 2/3 轮')
    expect(box).toHaveTextContent('1 轮失败')
    // It shares the title's line. Who changed the skill is in its facts.
    expect(box.parentElement).toContainElement(screen.getByRole('heading', { level: 1, name: skill.title }))
    expect(screen.getByText(/^Pi 写于 /)).toBeInTheDocument()
    // Keeping it is the trial's own button, not the page's confirm.
    expect(screen.queryByRole('button', { name: '确认' })).toBeNull()

    await userEvent.click(within(box).getByRole('button', { name: '退回' }))
    const dialog = await screen.findByRole('alertdialog', { name: '退回这次改动？' })
    await userEvent.type(within(dialog).getByLabelText('原因（选填）'), '说明变长了')
    await userEvent.click(within(dialog).getByRole('button', { name: '退回' }))
    await waitFor(() => expect(asked.rollback).toEqual({ name: 'go-table-tests', reason: '说明变长了' }))

    await userEvent.click(within(box).getByRole('button', { name: '转正' }))
    await waitFor(() => expect(asked.verify).toEqual({ path: '/skills/go-table-tests/SKILL.md', verify: true }))
  })

  it('says how the last trial ended', async () => {
    const ended = [
      [{ status: 'kept', ended_by: 'process:skill-trial' }, '上次改动试用期满，已自动转正'],
      [{ status: 'kept', ended_by: 'human:alice', reason: 'changed by hand' }, '上次改动在试用期间被 alice 手动修改，已转正'],
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
      // A row of the facts, above the text.
      const ended = await screen.findByText(words)
      expect(ended.closest('dl')).toHaveTextContent('试用')
      expect(screen.queryByRole('group', { name: '试用' })).toBeNull()
      unmount()
    }
  })
})
