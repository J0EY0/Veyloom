import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import type { SkillUse } from '@/api/types'
import { stubApi } from '@/test/fetch'
import { renderWithProviders } from '@/test/render'
import { LibraryPage } from './LibraryPage'
import { agent, routes, skill } from './library/libraryTesting'

// A skill's facts keep to a line each however much there is (docs/webui.md
// 4.10): a few agents and turns as chips, and a chip for the rest, which
// opens the whole list.

const agents = Array.from({ length: 6 }, (_, i) => agent(`a${i}`, `Agent ${i}`, i % 2 === 0 ? 'codex' : 'pi', ['go-table-tests']))

function uses(n: number): SkillUse[] {
  return Array.from({ length: n }, (_, i) => ({
    turn_id: `x${i}`,
    status: i === 1 ? 'failed' : 'done',
    runtime: 'claude',
    started_at: '2026-09-21T02:00:00Z',
    room_id: 'r2',
    thread_id: `t${i}`,
    topic_number: 100 - i,
    member_name: 'Writer',
    project_id: 'p2',
    project_name: 'Docs site',
  }))
}

function show(n: number) {
  stubApi(
    routes({
      '/library/page': { page: { ...skill, installed: agents.map((a) => ({ id: a.id, name: a.name })) } },
      '/library/usage': { uses: uses(n) },
      '/agents': { agents },
    }),
  )
  renderWithProviders(<LibraryPage />, { route: '/library/skills/go-table-tests/SKILL.md', path: '/library/*' })
}

describe('a skill with many agents and turns', () => {
  it('shows three agents and a chip for the rest, which lists them all', async () => {
    show(1)
    const facts = (await screen.findByText('已安装到')).closest('dl') as HTMLElement
    for (const name of ['Agent 0', 'Agent 1', 'Agent 2']) expect(within(facts).getByText(name)).toBeInTheDocument()
    expect(within(facts).queryByText('Agent 3')).toBeNull()
    await userEvent.click(within(facts).getByRole('button', { name: '已安装的全部 6 个 Agent' }))
    const list = await screen.findByRole('list', { name: '已安装到' })
    expect(within(list).getAllByRole('listitem')).toHaveLength(6)
    expect(list).toHaveTextContent('Agent 5')
    expect(list).toHaveTextContent('Codex')
  })

  it('shows the latest two turns and a chip for all of them, listed with when', async () => {
    show(12)
    const facts = (await screen.findByText('使用记录')).closest('dl') as HTMLElement
    expect(await within(facts).findAllByRole('link', { name: /Docs site · 话题/ })).toHaveLength(2)
    await userEvent.click(within(facts).getByRole('button', { name: '使用记录 · 全部 12 轮' }))
    const list = await screen.findByRole('list', { name: '使用记录' })
    const rows = within(list).getAllByRole('listitem')
    expect(rows).toHaveLength(12)
    expect(within(rows[11]).getByRole('link', { name: 'Docs site · 话题 #89' })).toHaveAttribute('href', '/rooms/r2?thread=t11')
    expect(rows[0].querySelector('time')).not.toBeNull()
    expect(screen.queryByText(/只列出最近/)).toBeNull()
  })

  it('says so when the turns read may not be all', async () => {
    show(50)
    const facts = (await screen.findByText('使用记录')).closest('dl') as HTMLElement
    await userEvent.click(await within(facts).findByRole('button', { name: '使用记录 · 50+ 轮' }))
    expect(await screen.findByText('只列出最近 50 轮')).toBeInTheDocument()
  })

  it('needs no chip for the rest when all fit', async () => {
    show(2)
    const facts = (await screen.findByText('使用记录')).closest('dl') as HTMLElement
    expect(await within(facts).findAllByRole('link', { name: /Docs site · 话题/ })).toHaveLength(2)
    expect(within(facts).queryByRole('button', { name: /^使用记录/ })).toBeNull()
  })
})
