import type { Agent, WikiCatalog, WikiPage, WikiPageInfo } from '@/api/types'
import { project } from '@/test/fixtures'

// What the skill library's tests read: a library with skills of two teams
// and of none, a pattern, a skill's page, and agents to install for.

export function info(path: string, type: string, title: string, overrides: Partial<WikiPageInfo> = {}): WikiPageInfo {
  return { path, type, title, tags: [], status: 'stable', tier: 'human-reviewed', modified: '2026-09-21T01:00:00Z', resident: false, ...overrides }
}

export const catalog: WikiCatalog = {
  pages: [
    info('/skills/go-table-tests/SKILL.md', 'Skill', 'Go table tests', { team: 'veyloom' }),
    info('/skills/release-notes/SKILL.md', 'Skill', 'Release notes', { team: 'docs-site' }),
    info('/skills/old-habit/SKILL.md', 'Skill', 'Old habit'),
    info('/patterns/copy-paste-tests.md', 'Pattern', 'Copy-pasted tests drift apart', { tier: 'unverified' }),
  ],
  dirs: [
    { name: 'skills', type: 'Skill' },
    { name: 'patterns', type: 'Pattern' },
  ],
  folder: '/state/wiki/library',
  history: true,
  teams: [
    { slug: 'veyloom', project_id: 'p1', name: 'Veyloom', room_id: 'r1' },
    { slug: 'docs-site', project_id: 'p2', name: 'Docs site', room_id: 'r2' },
  ],
}

export const skill: WikiPage = {
  ...catalog.pages[0],
  body: 'Write the cases as a table.',
  hash: 'h',
  file: '/state/wiki/library/skills/go-table-tests/SKILL.md',
  verified: [],
  sources: [{ id: 'p1', resource: '/patterns/copy-paste-tests.md', title: 'Copy-pasted tests', page: '/patterns/copy-paste-tests.md' }],
  backlinks: [],
}

export function routes(extra: Record<string, unknown> = {}) {
  return {
    '/library': { wiki: catalog },
    '/library/history': { commits: [] },
    '/users': { users: [] },
    '/projects': { projects: [project('p1', 'Veyloom'), project('p2', 'Docs site')] },
    ...extra,
  }
}

export function agent(id: string, name: string, runtime: string, skills: string[] = []): Agent {
  return {
    id,
    name,
    avatar: '',
    machine_id: 'w1',
    machine_name: 'laptop',
    projects: [],
    runtime,
    model: '',
    role_card: '',
    permission_preset: 'read_only',
    runtime_options: null,
    skills,
    created_at: '2026-09-22T00:00:00Z',
    updated_at: '2026-09-22T00:00:00Z',
  }
}

// A market's worth: skills of two teams and of none, one on trial, one kept
// for Codex, one retired; installed for agents as the agents say.
export const market: WikiCatalog = {
  ...catalog,
  pages: [
    info('/skills/go-table-tests/SKILL.md', 'Skill', 'Go table tests', { team: 'veyloom', description: 'Write the cases as a table.', on_trial: true }),
    info('/skills/go-table-tests/references/naming.md', 'Reference', 'Naming cases'),
    info('/skills/release-notes/SKILL.md', 'Skill', 'Release notes', { team: 'docs-site', description: 'Notes from the commits.', tags: ['runtime-codex'] }),
    info('/skills/old-habit/SKILL.md', 'Skill', 'Old habit', { status: 'deprecated' }),
    info('/patterns/copy-paste-tests.md', 'Pattern', 'Copy-pasted tests drift apart', {
      description: 'Each copy is fixed alone.',
      generated_by: 'codex/default',
      generated_at: '2026-09-21T01:00:00Z',
    }),
  ],
}

export const agents = [agent('a1', 'Coder', 'codex', ['go-table-tests', 'release-notes']), agent('a2', 'Writer', 'claude', ['go-table-tests'])]
