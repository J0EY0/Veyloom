import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Usage, UsageRange } from '@/api/types'
import { stubApi } from '@/test/fetch'
import { project } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { pickOption } from '@/test/select'
import { UsagePage } from './UsagePage'

// Today, three turns: the coder's spent the most.
function today(range: UsageRange = 'today'): Usage {
  return {
    range,
    step: 'turn',
    total: { input_tokens: 4_000, cache_read_tokens: 12_000, cache_write_tokens: 0, output_tokens: 2_000 },
    turns: 3,
    points: [
      {
        at: '2026-09-26T02:00:00Z',
        tokens: 3_000,
        output: 500,
        duration_ms: 60_000,
        turns: 1,
        turn_id: 'x1',
        member_id: 'lead',
        member: 'Lead',
        room_id: 'r1',
        thread_number: 1,
      },
      {
        at: '2026-09-26T02:01:00Z',
        tokens: 12_000,
        output: 1_000,
        duration_ms: 120_000,
        turns: 1,
        turn_id: 'x2',
        member_id: 'coder',
        member: 'Coder',
        room_id: 'r1',
        thread_number: 2,
      },
      {
        at: '2026-09-26T02:30:00Z',
        tokens: 3_000,
        output: 500,
        duration_ms: 30_000,
        turns: 1,
        turn_id: 'x3',
        member_id: 'tester',
        member: 'Tester',
        room_id: 'r1',
        thread_number: 3,
      },
    ],
    runtimes: [
      { runtime: 'claude', tokens: 15_000, turns: 2 },
      { runtime: 'codex', tokens: 3_000, turns: 1 },
    ],
    members: [
      { member_id: 'coder', name: 'Coder', model: 'opus', runtime: 'claude', project_id: 'p1', project_name: 'Linkkeeper', tokens: 12_000, turns: 1 },
      { member_id: 'lead', name: 'Lead', runtime: 'claude', project_id: 'p1', project_name: 'Linkkeeper', tokens: 3_000, turns: 1 },
      { member_id: 'tester', name: 'Tester', runtime: 'codex', project_id: 'p2', project_name: 'Other', tokens: 3_000, turns: 1 },
    ],
    works: [
      { chain: 'c1', kind: 'chat', room_id: 'r1', project_name: 'Linkkeeper', thread_number: 1, title: 'Add tags', tokens: 15_000, turns: 2 },
      { kind: 'setup', room_id: 'r2', project_name: 'Other', tokens: 3_000, turns: 1 },
    ],
  }
}

function open(usage: (params: URLSearchParams) => Usage = (params) => today(params.get('range') as UsageRange)) {
  const calls = stubApi({
    '/usage': (req: Request) => ({ usage: usage(new URL(req.url).searchParams) }),
    '/projects': { projects: [project('p1', 'Linkkeeper'), project('p2', 'Other')] },
    '/agents': { agents: [] },
  })
  const view = renderWithProviders(<UsagePage />, { route: '/usage' })
  return { ...view, calls }
}

const figure = (name: string) => within(screen.getByRole('group', { name: '总览' })).getByRole('region', { name })

describe('UsagePage', () => {
  it('sums the range up in four figures', async () => {
    open()
    await screen.findByRole('group', { name: '总览' })
    expect(figure('Token')).toHaveTextContent('1.8万')
    expect(figure('缓存命中')).toHaveTextContent('75%')
    expect(figure('输出')).toHaveTextContent('2000')
    expect(figure('轮次')).toHaveTextContent('3')
  })

  it('charts each turn, with a table a screen reader gets', async () => {
    open()
    const chart = await screen.findByRole('region', { name: '按轮次' })
    const rows = within(within(chart).getByRole('table', { name: '按轮次' })).getAllByRole('row')
    expect(rows).toHaveLength(4)
    expect(
      within(rows[2])
        .getAllByRole('cell')
        .map((cell) => cell.textContent),
    ).toEqual(['Coder · #2', '12,000', '2 分'])
    // The chart shows the time taken on asking.
    await userEvent.click(within(chart).getByRole('tab', { name: '用时' }))
    expect(within(chart).getByRole('tab', { name: '用时' })).toHaveAttribute('aria-selected', 'true')
  })

  it('shares the tokens out by runtime, member and piece of work', async () => {
    open()
    const runtimes = await screen.findByRole('region', { name: '运行时' })
    expect(
      within(runtimes)
        .getAllByRole('listitem')
        .map((row) => row.textContent),
    ).toEqual(['Claude Code2 轮1.5万', 'Codex1 轮3000'])
    // The shares over the bar; each runtime named once, in its row below.
    expect(runtimes).toHaveTextContent(/^运行时83%17%Claude Code2 轮1\.5万Codex1 轮3000$/)

    const members = screen.getByRole('region', { name: '成员' })
    const first = within(members).getAllByRole('listitem')[0]
    // Every project shown: a member says whose it is.
    expect(first).toHaveTextContent('CoderLinkkeeper1.2万67%')

    const works = screen.getByRole('region', { name: '按任务' })
    expect(within(works).getByRole('link', { name: 'Add tags' })).toHaveAttribute('href', '/rooms/r1/tasks/c1')
    // A project's setup is there too, but opens nothing.
    expect(within(works).getByText('项目初始化').closest('a')).toBeNull()
  })

  it('keeps the range and the project in the address and asks for them', async () => {
    const { router, calls } = open()
    await screen.findByRole('group', { name: '总览' })
    expect(calls).toContainEqual(expect.stringMatching(/^GET \/usage\?range=today&tz=[^&]+$/))

    await userEvent.click(screen.getByRole('tab', { name: '7 天' }))
    expect(router.state.location.search).toBe('?range=7d')
    expect(await screen.findByRole('tab', { name: '7 天', selected: true })).toBeInTheDocument()
    expect(calls).toContainEqual(expect.stringMatching(/^GET \/usage\?range=7d&tz=[^&]+$/))

    await pickOption('项目', 'Other')
    expect(router.state.location.search).toBe('?range=7d&project=p2')
    expect(calls).toContainEqual(expect.stringMatching(/^GET \/usage\?range=7d&tz=[^&]+&project=p2$/))
    // One project: members say their model instead.
    const members = await screen.findByRole('region', { name: '成员' })
    expect(within(members).getAllByRole('listitem')[0]).toHaveTextContent('Coderopus')
  })

  it('says so when nothing ran in the range', async () => {
    open(() => ({ ...today(), turns: 0, points: [], runtimes: [], members: [], works: [] }))
    expect(await screen.findByText('这段时间还没有轮次')).toBeInTheDocument()
  })
})

describe('UsagePage in a narrow page', () => {
  afterEach(() => vi.restoreAllMocks())

  it('picks the range from a menu like the project', async () => {
    // A 22.5rem page, a phone's: the three ranges beside the project would be cut.
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 0, 360, 800))
    const { router, calls } = open()
    await screen.findByRole('group', { name: '总览' })
    expect(screen.queryByRole('tablist', { name: '时间范围' })).toBeNull()
    await pickOption('时间范围', '7 天')
    expect(router.state.location.search).toBe('?range=7d')
    expect(calls).toContainEqual(expect.stringMatching(/^GET \/usage\?range=7d&tz=[^&]+$/))
  })
})
