import type { WikiCatalog, WikiCommit, WikiPage, WikiPageInfo } from '@/api/types'
import { renderWithProviders } from '@/test/render'
import { WikiView } from './WikiView'

// What the Wiki tab's tests read: a project's wiki of a few pages, one page
// in full, a change, and the routes that answer for them.

export function info(path: string, type: string, title: string, overrides: Partial<WikiPageInfo> = {}): WikiPageInfo {
  return { path, type, title, tags: [], status: 'stable', tier: 'unverified', modified: '2026-09-21T01:00:00Z', resident: false, ...overrides }
}

export const pages = [
  info('/facts/port.md', 'Fact', 'The hub listens on 7788', {
    description: 'Unless --addr says otherwise.',
    generated_by: 'codex/default',
    generated_at: '2026-09-21T01:00:00Z',
  }),
  info('/conventions/naming.md', 'Convention', '命名规范', { resident: true, tier: 'human-reviewed', vouched_at: '2026-09-21T01:00:00Z', tags: ['resident'] }),
  info('/facts/old.md', 'Fact', 'Old news', { status: 'deprecated' }),
  info('/notes/misc.md', 'Note', 'Misc'),
  info('/memory.md', 'Memory', 'Project memory'),
]

export const catalog: WikiCatalog = {
  pages,
  dirs: [
    { name: 'decisions', type: 'Decision' },
    { name: 'conventions', type: 'Convention' },
    { name: 'facts', type: 'Fact' },
  ],
  folder: '/state/wiki/projects/veyloom',
  history: true,
}

export const port: WikiPage = {
  ...pages[0],
  body: 'Set in [the config](/modules/config.md). See [the docs](https://example.com).',
  hash: 'h1',
  file: '/state/wiki/projects/veyloom/facts/port.md',
  verified: [],
  sources: [
    { id: 'veyloom-turn', resource: 'veyloom://turns/x1', title: 'This turn', room_id: 'r1', thread_id: 't3', topic_number: 3, turn_id: 'x1' },
    { id: 'goose', resource: 'https://pressly.github.io/goose/', title: 'goose' },
  ],
  backlinks: [{ path: '/modules/config.md', title: 'Config' }],
}

export const commit: WikiCommit = {
  sha: 'abc',
  author: 'codex/default',
  at: '2026-09-21T01:00:00Z',
  subject: 'Writer in topic #3',
  changes: [{ kind: 'Creation', path: '/facts/port.md', title: 'The hub listens on 7788', text: '[The hub listens on 7788](/facts/port.md) by codex/default' }],
  member: 'Writer',
  thread_id: 't3',
  topic_number: 3,
  undoable: true,
}

export function routes(extra: Record<string, unknown> = {}) {
  return {
    '/projects/p1/wiki': { wiki: catalog },
    '/projects/p1/wiki/history': { commits: [commit] },
    '/rooms/r1/members': { members: [{ id: 'm1', display_name: 'Claude', enabled: true }] },
    '/users': { users: [{ id: 'u1', name: 'alice' }] },
    ...extra,
  }
}

export function renderWiki(rest: string, onOpenThread: (threadId: string) => void = () => {}) {
  return renderWithProviders(<WikiView space={{ kind: 'project', projectId: 'p1', roomId: 'r1' }} rest={rest} onOpenThread={onOpenThread} />, {
    route: `/rooms/r1/wiki/${rest}`,
  })
}
