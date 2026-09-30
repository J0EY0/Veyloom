import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { stubApi } from '@/test/fetch'
import { renderWithProviders } from '@/test/render'
import { LibraryPage } from './LibraryPage'
import { routes, skill } from './library/libraryTesting'

// A person takes a skill out of use and puts it back, or removes it, from
// its page's "…" menu (docs/design.md 5.15).
describe('managing a skill of the library', () => {
  it('retires a skill and puts it back in use', async () => {
    const asked: unknown[] = []
    let page = skill
    stubApi(
      routes({
        '/library/page': () => ({ page }),
        '/library/usage': { uses: [] },
        '/library/retire': async (req: Request) => {
          const body = (await req.json()) as { retired: boolean }
          asked.push(body)
          page = { ...skill, status: body.retired ? 'deprecated' : 'stable' }
          return { page }
        },
      }),
    )
    renderWithProviders(<LibraryPage />, { route: '/library/skills/go-table-tests/SKILL.md', path: '/library/*' })
    expect(await screen.findByRole('heading', { name: 'Go table tests' })).toBeInTheDocument()
    await userEvent.click(within(await screen.findByRole('article')).getByRole('button', { name: '更多' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: '停用' }))
    // Retired, it says so, and can be put back.
    expect(await screen.findByText('已停用')).toBeInTheDocument()
    await userEvent.click(within(await screen.findByRole('article')).getByRole('button', { name: '更多' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: '恢复使用' }))
    await waitFor(() => expect(screen.queryByText('已停用')).toBeNull())
    expect(asked).toEqual([
      { name: 'go-table-tests', retired: true },
      { name: 'go-table-tests', retired: false },
    ])
  })

  it('asks before removing a skill, saying whom it is taken off, then goes back to the list', async () => {
    const removed: unknown[] = []
    stubApi(
      routes({
        '/library/page': { page: { ...skill, installed: [{ id: 'a1', name: 'Coder' }] } },
        '/library/usage': { uses: [] },
        '/library/delete': async (req: Request) => {
          removed.push(await req.json())
          return new Response(null, { status: 204 })
        },
      }),
    )
    renderWithProviders(<LibraryPage />, { route: '/library/skills/go-table-tests/SKILL.md', path: '/library/*' })
    expect(await screen.findByRole('heading', { name: 'Go table tests' })).toBeInTheDocument()
    await userEvent.click(within(await screen.findByRole('article')).getByRole('button', { name: '更多' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: '删除技能…' }))
    const ask = await screen.findByRole('alertdialog', { name: '删除技能 go-table-tests？' })
    expect(ask).toHaveTextContent('并从已安装的 1 个 Agent 上卸下')
    await userEvent.click(within(ask).getByRole('button', { name: '删除' }))
    await waitFor(() => expect(removed).toEqual([{ name: 'go-table-tests' }]))
    // Back to the list of skills.
    expect(await screen.findByRole('searchbox', { name: '搜索技能' })).toBeInTheDocument()
    expect(screen.queryByRole('alertdialog')).toBeNull()
  })
})
