import { vi } from 'vitest'
import type { Branches, Project } from '@/api/types'
import { stubApi } from '@/test/fetch'
import { project } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { BranchesView } from './BranchesView'

// What the branch tab's tests stand on: a project with its steps written
// down, and its branches as the hub sends them.

export const app: Project = {
  ...project('p1', 'App', '/src/app'),
  main_room_id: 'r1',
  initialized_at: '2026-09-23T01:00:00Z',
  workspace_copy: ['.env', 'docs/'],
  workspace_run: 'npm ci',
  setup_thread_id: 't9',
}

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

export function stub(extra: Record<string, unknown> = {}, over: Partial<Project> = {}, shape: Branches = branches) {
  return stubApi({
    '/projects': { projects: [{ ...app, ...over }] },
    '/projects/p1/branches': { branches: shape },
    ...extra,
  })
}

export function renderView() {
  return renderWithProviders(<BranchesView projectId="p1" roomId="r1" onOpenThread={vi.fn()} />)
}
