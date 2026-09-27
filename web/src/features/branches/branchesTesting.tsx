import { screen } from '@testing-library/react'
import { vi } from 'vitest'
import type { Branches, Member, Project, Task } from '@/api/types'
import { MembersPanel } from '@/features/members/MembersPanel'
import { stubApi } from '@/test/fetch'
import { project, room } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'

// What the branches' tests stand on: a project with its steps written
// down, its members, and its branches as the hub sends them, shown where a
// person meets them, in the chat's info (docs/design.md 5.21).

export const app: Project = {
  ...project('p1', 'App', '/src/app'),
  main_room_id: 'r1',
  initialized_at: '2026-09-23T01:00:00Z',
  workspace_copy: ['.env', 'docs/'],
  workspace_run: 'npm ci',
  setup_thread_id: 't9',
  leader_id: 'm0',
}

function member(id: string, name: string): Member {
  return {
    id,
    room_id: 'r1',
    agent_id: `a-${id}`,
    machine_id: 'w1',
    display_name: name,
    repo_path: '/src/app',
    branch_mode: 'worktree',
    model: '',
    permission_preset: '',
    enabled: true,
    created_at: '2026-09-23T00:00:00Z',
  }
}

// The leader works in the checkout; the others each in a worktree.
export const members = [member('m0', 'Lead'), member('m1', 'Coder'), member('m2', 'Tester'), member('m3', 'Writer')]

export const branches: Branches = {
  main: { repo_path: '/src/app', git: true, branch: 'main', changed: [{ path: 'notes.md', status: 'M', added: 1, deleted: 0, uncommitted: true }] },
  members: [
    {
      member_id: 'm1',
      name: 'Coder',
      branch: 'veyloom/coder',
      dir: '/wt/app/coder',
      prepared: true,
      busy: false,
      draft: 'add the feature',
      status: {
        branch: 'veyloom/coder',
        base: 'main',
        ahead: 2,
        behind: 1,
        uncommitted: 1,
        files: [
          { path: 'src/api.ts', status: 'M', added: 3, deleted: 1, uncommitted: true },
          { path: 'src/new.ts', status: 'A', added: 9, deleted: 0 },
        ],
      },
    },
    {
      member_id: 'm2',
      name: 'Tester',
      branch: 'veyloom/tester',
      dir: '/wt/app/tester',
      prepared: true,
      busy: true,
      status: {
        branch: 'veyloom/tester',
        ahead: 0,
        behind: 0,
        uncommitted: 1,
        files: [{ path: 'src/api.ts', status: 'M', added: 1, deleted: 1, uncommitted: true }],
      },
    },
    { member_id: 'm3', name: 'Writer', prepared: false, busy: false },
  ],
  overlaps: [{ path: 'src/api.ts', members: ['m1', 'm2'] }],
}

// The piece of work waiting on Coder's branch.
export const coderTask: Task = {
  chain: 'c1',
  thread_id: 't12',
  thread_number: 12,
  member_id: 'm1',
  title: '给 delete 加 --tag',
  state: 'merge',
  started_at: '2026-09-23T01:00:00Z',
  ended_at: '2026-09-23T01:05:00Z',
  turns: 1,
}

export function stub(extra: Record<string, unknown> = {}, over: Partial<Project> = {}, shape: Branches = branches) {
  return stubApi({
    '/rooms/r1': { room: room('r1', 'p1', 'main') },
    '/rooms/r1/members': { members },
    '/rooms/r1/approvals': { approvals: [] },
    '/rooms/r1/turns': { turns: [] },
    '/rooms/r1/tasks': { tasks: [coderTask] },
    '/agents': { agents: [] },
    '/machines': { machines: [] },
    '/projects': { projects: [{ ...app, ...over }] },
    '/projects/p1/branches': { branches: shape },
    ...extra,
  })
}

export function renderView() {
  return renderWithProviders(<MembersPanel roomId="r1" roomName="main" onClose={vi.fn()} onOpenThread={vi.fn()} />)
}

// rowOf is a member's row in the chat's info, once its branch is shown.
export async function rowOf(name: string): Promise<HTMLElement> {
  return (await screen.findByText(name, { selector: '[data-slot="item-title"] span' })).closest('[role="listitem"]') as HTMLElement
}
