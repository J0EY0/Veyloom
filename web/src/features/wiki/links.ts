import type { WikiSpace } from '@/api/wiki'

// Where the wikis live in the app's URLs (docs/webui.md §3): the Wiki tab
// of a chat is /rooms/:roomId/wiki, the skill library /library; a page is
// under either at its own path, the changes at /changes (pages all end in
// .md, so the two never meet).

export function wikiHref(roomId: string, page = ''): string {
  return `/rooms/${roomId}/wiki${page}`
}

export function wikiChangesHref(roomId: string): string {
  return `/rooms/${roomId}/wiki/changes`
}

// wikisHref is the Wiki page of the sidebar (docs/design.md 5.18): every
// project's wiki, or with a project its own, at a page of it.
export function wikisHref(projectId = '', page = ''): string {
  return projectId ? `/wiki/${projectId}${page}` : '/wiki'
}

// globalMemoryHref is the global memory, the one every project shares:
// General settings, with its dialog open (docs/design.md 5.19).
export const globalMemoryHref = '/settings/general?memory=personal'

// pageHref is a page of a space, or with no page its front. The global
// memory has one page, kept in Settings. A project's wiki read standalone
// keeps to the Wiki page's addresses.
export function pageHref(space: WikiSpace, page = ''): string {
  if (space.kind === 'personal') return globalMemoryHref
  if (space.kind === 'library') return `/library${page}`
  return space.standalone ? wikisHref(space.projectId, page) : wikiHref(space.roomId, page)
}

export function changesHref(space: WikiSpace): string {
  return pageHref(space, '/changes')
}

// memoryPage is the page a memory is kept in (docs/design.md 5.16). The
// project's is in its wiki, which lists it apart from the other pages.
export const memoryPage = '/memory.md'

// conventionsPage is where a project's wiki keeps its own conventions,
// what goes into it and how its pages are written (docs/design.md 5.12):
// the page people and its maintainer keep, listed apart like the memory.
export const conventionsPage = '/conventions/wiki.md'

// memoryHref is the project memory, kept in the Wiki tab (docs/design.md
// 5.16).
export function memoryHref(space: WikiSpace): string {
  return pageHref(space, '/memory')
}

// graphHref is the wiki's relation graph (docs/design.md 5.17), focused on
// a page when one is given.
export function graphHref(space: WikiSpace, focus = ''): string {
  const href = pageHref(space, '/graph')
  return focus ? `${href}?${new URLSearchParams({ focus }).toString()}` : href
}

// overviewHref is the wiki's front page by name: on a narrow screen the
// wiki's root shows its list of pages, this the page itself.
export function overviewHref(space: WikiSpace): string {
  return pageHref(space, '/overview')
}

// topicHref is a topic of some chat, opened in its room.
export function topicHref(roomId: string, threadId: string): string {
  return `/rooms/${roomId}?thread=${threadId}`
}

// WikiRoute is what the part of the URL after /wiki names.
export type WikiRoute = { kind: 'overview' } | { kind: 'changes' } | { kind: 'memory' } | { kind: 'graph' } | { kind: 'page'; path: string }

// The memory's page, /memory.md, a link or a search may lead to, opens as
// the memory too.
export function wikiRoute(rest: string): WikiRoute {
  const trimmed = rest.replace(/^\/+|\/+$/g, '')
  if (trimmed === 'changes') return { kind: 'changes' }
  if (trimmed === 'graph') return { kind: 'graph' }
  if (trimmed === 'memory' || trimmed === 'memory.md') return { kind: 'memory' }
  if (trimmed.endsWith('.md')) return { kind: 'page', path: `/${trimmed}` }
  return { kind: 'overview' }
}

// resolvePage turns a link in a page into the page it names, the way OKF
// reads links (internal/wiki/okf, Resolve): from the wiki's root when it
// starts with a slash, else from the folder the page is in. Anything that
// is not a page of the wiki (a web address, an anchor, a file that is not
// markdown) is undefined.
export function resolvePage(from: string, href: string): string | undefined {
  const target = bundlePath(from, href)
  return target?.endsWith('.md') ? target : undefined
}

// resolveFile is the file a link or a picture in a page names, one the wiki
// keeps beside its pages under /files/ (docs/design.md 5.16).
export function resolveFile(from: string, href: string): string | undefined {
  const target = bundlePath(from, href)
  return target?.startsWith('/files/') && !target.endsWith('.md') ? target : undefined
}

// bundlePath reads a link's target as a path in the wiki; undefined for a
// web address, an anchor, or one that climbs out of the wiki.
function bundlePath(from: string, href: string): string | undefined {
  if (href === '' || href.startsWith('#') || /^[a-z][a-z0-9+.-]*:/i.test(href)) return undefined
  const target = href.split('#')[0].split('?')[0]
  const parts = target.startsWith('/') ? [] : from.split('/').slice(1, -1)
  for (const part of target.split('/')) {
    if (part === '' || part === '.') continue
    if (part === '..') {
      if (parts.length === 0) return undefined
      parts.pop()
    } else {
      parts.push(decodeURIComponent(part))
    }
  }
  return parts.length > 0 ? `/${parts.join('/')}` : undefined
}
