import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import type { Member, Work, WorkTurn } from '@/api/types'
import { stubApi } from '@/test/fetch'
import { project, room, user } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { TasksView } from './TasksView'

function member(id: string, display_name: string, branch?: string): Member {
  return {
    id,
    room_id: 'r1',
    agent_id: `agent-${id}`,
    machine_id: 'w1',
    display_name,
    repo_path: '',
    branch_mode: 'worktree',
    model: '',
    permission_preset: '',
    enabled: true,
    created_at: '2026-09-26T00:00:00Z',
    branch,
  }
}

const zero = { input_tokens: 0, cache_read_tokens: 0, cache_write_tokens: 0, output_tokens: 0 }

function workTurn(id: string, member_id: string, from: string, to: string, overrides: Partial<WorkTurn> = {}): WorkTurn {
  return {
    id,
    member_id,
    thread_id: 't1',
    thread_number: 3,
    status: 'done',
    started_at: `2026-09-26T10:${from}Z`,
    ended_at: `2026-09-26T10:${to}Z`,
    usage: zero,
    kind: 'task',
    waited_ms: 0,
    files: 0,
    ...overrides,
  }
}

// The lead split the work between the coder and the tester, the tester
// handed a question on to the coder, and the lead summed up; the coder's
// branch was merged, the tester's reset.
const work: Work = {
  chain: 'c1',
  room_id: 'r1',
  thread_id: 't1',
  thread_number: 3,
  title: 'Add tags to links',
  ask: '@Lead add tags to links, and test them',
  asked_by: 'u1',
  asked: ['lead'],
  running: false,
  started_at: '2026-09-26T10:00:00Z',
  ended_at: '2026-09-26T10:03:30Z',
  turns: [
    workTurn('x1', 'lead', '00:00', '00:20', { kind: 'split', woke: ['coder', 'tester'] }),
    workTurn('x2', 'coder', '00:20', '03:00', {
      title: 'Add the tag table',
      waits: [{ from: '2026-09-26T10:01:00Z', to: '2026-09-26T10:02:00Z' }],
      waited_ms: 60_000,
      files: 3,
      usage: { ...zero, input_tokens: 12_000 },
    }),
    workTurn('x3', 'tester', '00:20', '00:30', { kind: 'handoff', woke: ['coder'], relay: true }),
    workTurn('x4', 'lead', '03:00', '03:30', { kind: 'sumup' }),
  ],
  usage: { ...zero, input_tokens: 12_000, output_tokens: 345 },
  waited_ms: 60_000,
  events: [
    { kind: 'merged', commit: 'abcdef1234567', members: ['coder'], at: '2026-09-26T10:05:00Z' },
    { kind: 'reset', ref: 'refs/veyloom/archive/tester/1', members: ['tester'], at: '2026-09-26T10:06:00Z' },
  ],
}

function stub(found: Work | Response = work, extra: Record<string, unknown> = {}) {
  return stubApi({
    ...extra,
    '/works/c1': found instanceof Response ? found : { work: found },
    '/rooms/r1': { room: room('r1', 'p1', 'main') },
    '/projects': { projects: [{ ...project('p1', 'Linkkeeper'), leader_id: 'lead' }] },
    '/rooms/r1/members': { members: [member('lead', 'Lead'), member('coder', 'Coder'), member('tester', 'Tester', 'veyloom/tester')] },
    '/users': { users: [user('u1', 'alice')] },
    '/agents': { agents: [] },
  })
}

function open() {
  const onOpenThread = vi.fn()
  renderWithProviders(<TasksView roomId="r1" chain="c1" onOpenThread={onOpenThread} />, { route: '/rooms/r1/tasks/c1' })
  return onOpenThread
}

describe('WorkView', () => {
  it('says what was asked, by whom, and how it went in figures', async () => {
    stub()
    const onOpenThread = open()

    expect(await screen.findByRole('heading', { level: 1, name: 'Add tags to links' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '任务' })).toHaveAttribute('href', '/rooms/r1/tasks')
    expect(screen.getByText('完成')).toBeInTheDocument()
    expect(screen.getByText('alice 发起')).toBeInTheDocument()
    // The member asked, called the leader once the project says so.
    expect(await screen.findByText('组长Lead')).toBeInTheDocument()
    expect(screen.getByText('@Lead add tags to links, and test them')).toBeInTheDocument()

    const figures = Object.fromEntries(screen.getAllByRole('term').map((term) => [term.textContent, term.nextElementSibling?.textContent]))
    expect(figures).toEqual({ 用时: '3 分 30 秒', Token: '1.2万', 轮次: '4', 等你审批: '1 分', 合并: 'abcdef1' })

    await userEvent.click(screen.getByRole('button', { name: '在话题里看 #3' }))
    expect(onOpenThread).toHaveBeenCalledWith('t1')
  })

  it('draws a row a turn, saying what each did', async () => {
    stub()
    open()
    const table = await screen.findByRole('table', { name: '每一轮' })
    const rows = within(table).getAllByRole('row').slice(1)
    expect(rows.map((row) => within(row).getByRole('rowheader').textContent)).toEqual([
      'L' + 'Lead拆分任务#3',
      'C' + 'CoderAdd the tag table#3',
      'T' + 'Tester交给 Coder',
      'L' + 'Lead汇总答复#3',
    ])
    // Right of the bar: how long, how long it waited on a person, tokens.
    expect(
      within(rows[1])
        .getAllByRole('cell')
        .map((cell) => cell.textContent),
    ).toEqual([expect.stringMatching(/–/), '2 分 40 秒', '1 分', '1.2万'])
  })

  it('lists what became of the branches the work changed', async () => {
    stub()
    open()
    const merged = (await screen.findByText('合并进主线')).closest('li')!
    expect(merged).toHaveTextContent('合并进主线abcdef1，带着 Coder 的改动')
    const reset = (await screen.findByText('重置到主线')).closest('li')!
    expect(reset).toHaveTextContent('veyloom/tester重置到主线，原内容归档为refs/veyloom/archive/tester/1')
  })

  it('merges the work still waiting on a member’s branch', async () => {
    stub(work, {
      '/rooms/r1/tasks': {
        tasks: [
          {
            chain: 'c1',
            thread_id: 't2',
            thread_number: 2,
            member_id: 'coder',
            title: 'Add the tag table',
            state: 'merge',
            started_at: '2026-09-26T09:00:00Z',
            turns: 1,
          },
        ],
      },
      '/projects/p1/branches': {
        branches: {
          main: { repo_path: '/src/app', git: true, branch: 'main' },
          members: [
            {
              member_id: 'coder',
              name: 'Coder',
              branch: 'veyloom/coder',
              dir: '/wt/coder',
              prepared: true,
              busy: false,
              status: { branch: 'veyloom/coder', ahead: 1, behind: 0, uncommitted: 0, files: [{ path: 'tags.go', status: 'A', added: 9, deleted: 0 }] },
            },
          ],
        },
      },
    })
    open()
    await userEvent.click(await screen.findByRole('button', { name: '合并 Coder 的改动' }))
    const dialog = await screen.findByRole('dialog', { name: '把 Coder 的改动合并到 main' })
    expect(within(dialog).getByText('Add the tag table', { selector: 'li span' })).toBeInTheDocument()
  })

  it('says so when the work cannot be read', async () => {
    stub(Response.json({ error: 'no such work' }, { status: 404 }))
    open()
    expect(await screen.findByText('这件事没能读出来')).toBeInTheDocument()
  })
})
