import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import type { Member, Task } from '@/api/types'
import { stubApi } from '@/test/fetch'
import { user } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { TasksView } from './TasksView'

function member(id: string, display_name: string): Member {
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
  }
}

function task(chain: string, member_id: string, overrides: Partial<Task> = {}): Task {
  return {
    chain,
    thread_id: `t-${chain}`,
    thread_number: 1,
    member_id,
    title: `Task ${chain}`,
    state: 'done',
    started_at: '2026-09-26T09:00:00Z',
    ended_at: '2026-09-26T09:10:00Z',
    turns: 1,
    ...overrides,
  }
}

// A piece of work the lead split between the coder and the tester, and
// what the coder did before it.
const tasks: Task[] = [
  task('w1', 'lead', { title: 'Add tags', state: 'running', ended_at: undefined, parts: { done: 1, total: 2 } }),
  task('w1', 'coder', {
    thread_id: 't-w1',
    title: 'Add the tag table',
    state: 'running',
    ended_at: undefined,
    waiting: true,
    work: { chain: 'w1', thread_number: 1, member_id: 'lead', title: 'Add tags' },
  }),
  task('w1', 'tester', {
    thread_id: 't-w1',
    title: 'Test the tags',
    state: 'merge',
    work: { chain: 'w1', thread_number: 1, member_id: 'lead', title: 'Add tags' },
  }),
  task('d1', 'coder', { title: 'Merged work', outcome: { kind: 'merged', commit: 'abcdef1234567' }, ended_at: '2026-09-26T09:59:00Z' }),
  task('d2', 'coder', { title: 'Archived work', outcome: { kind: 'archived', ref: 'refs/veyloom/archive/coder/1' }, ended_at: '2026-09-26T09:58:00Z' }),
  task('d3', 'lead', { title: 'Wrote a page', outcome: { kind: 'wiki', page: '/conventions/tags.md' }, ended_at: '2026-09-26T09:57:00Z' }),
  task('d4', 'lead', { title: '', thread_number: 7, ended_at: '2026-09-26T09:56:00Z' }),
  task('d5', 'coder', { ended_at: '2026-09-26T09:55:00Z' }),
  task('d6', 'coder', { ended_at: '2026-09-26T09:54:00Z' }),
  task('d7', 'coder', { ended_at: '2026-09-26T09:53:00Z' }),
]

function stub(list: Task[] = tasks) {
  return stubApi({
    '/rooms/r1/tasks': { tasks: list },
    '/rooms/r1/members': { members: [member('lead', 'Lead'), member('coder', 'Coder'), member('tester', 'Tester')] },
    '/users': { users: [user('u1', 'alice')] },
    '/agents': { agents: [] },
  })
}

function open() {
  const onOpenThread = vi.fn()
  const view = renderWithProviders(<TasksView roomId="r1" chain="" onOpenThread={onOpenThread} />, { route: '/rooms/r1/tasks' })
  return { ...view, onOpenThread }
}

const cardTitles = (column: HTMLElement) =>
  within(column)
    .queryAllByRole('heading', { level: 3 })
    .map((h) => h.textContent)

describe('TasksView', () => {
  it('lays the tasks out by state, each card one member’s task', async () => {
    stub()
    const { onOpenThread } = open()

    const running = await screen.findByRole('region', { name: '进行中' })
    const merge = screen.getByRole('region', { name: '待合并' })
    const done = screen.getByRole('region', { name: '已完成' })
    expect(within(running).getByRole('heading', { level: 2 })).toHaveTextContent('进行中2')
    expect(cardTitles(running)).toEqual(expect.arrayContaining(['Add tags', 'Add the tag table']))
    expect(cardTitles(merge)).toEqual(['Test the tags'])

    // A card opens its piece of work; a split one shows how far its parts
    // are; a part says what it is part of.
    expect(within(running).getByRole('link', { name: 'Add tags' })).toHaveAttribute('href', '/rooms/r1/tasks/w1')
    expect(within(running).getByRole('img', { name: '子任务完成 1/2' })).toBeInTheDocument()
    const part = within(running).getByRole('link', { name: 'Add the tag table' }).closest('article')!
    expect(part).toHaveTextContent('属于Add tags')

    // A request waiting on a person opens its topic; work to merge goes to
    // the branches.
    await userEvent.click(within(running).getByRole('button', { name: '待审批' }))
    expect(onOpenThread).toHaveBeenCalledWith('t-w1')
    expect(within(merge).getByRole('link', { name: '合并' })).toHaveAttribute('href', '/rooms/r1/branches')

    // Done cards say what came of them: a commit, an archive, a page.
    expect(within(done).getByRole('link', { name: 'Merged work' }).closest('article')).toHaveTextContent('abcdef1')
    expect(within(done).getByRole('link', { name: 'Archived work' }).closest('article')).toHaveTextContent('已归档')
    expect(within(done).getByRole('link', { name: 'tags' })).toHaveAttribute('href', '/rooms/r1/wiki/conventions/tags.md')
    // A task nothing names goes by its topic.
    expect(within(done).getByRole('link', { name: '话题 #7' })).toBeInTheDocument()
  })

  it('folds the done tasks past the latest five', async () => {
    stub()
    open()
    const done = await screen.findByRole('region', { name: '已完成' })
    expect(cardTitles(done)).toHaveLength(5)
    await userEvent.click(within(done).getByRole('button', { name: /更早的 2 个/ }))
    expect(cardTitles(done)).toHaveLength(7)
    await userEvent.click(within(done).getByRole('button', { name: /收起/ }))
    expect(cardTitles(done)).toHaveLength(5)
  })

  it('groups by member and leaves out the members unticked', async () => {
    stub()
    const { router } = open()
    await screen.findByRole('region', { name: '进行中' })

    await userEvent.click(screen.getByRole('tab', { name: '按成员' }))
    expect(router.state.location.search).toBe('?group=member')
    const coder = await screen.findByRole('region', { name: 'Coder' })
    // Under way first; done ones do not fold in a member's column.
    expect(cardTitles(coder)[0]).toBe('Add the tag table')
    expect(cardTitles(coder)).toHaveLength(6)
    expect(screen.getByRole('region', { name: 'Lead' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Tester' })).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: '按成员筛选' }))
    await userEvent.click(await screen.findByRole('menuitemcheckbox', { name: 'Tester' }))
    // The menu stays open for the next one; closed, the board is back.
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('menu')).toBeNull()
    expect(screen.queryByRole('region', { name: 'Tester' })).toBeNull()
    expect(screen.getByRole('region', { name: 'Coder' })).toBeInTheDocument()
  })

  it('says so when there is nothing yet', async () => {
    stub([])
    open()
    expect(await screen.findByText('还没有任务')).toBeInTheDocument()
  })
})
