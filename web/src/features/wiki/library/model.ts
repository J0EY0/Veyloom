import type { Agent, WikiCatalog, WikiPageInfo, WikiStatus } from '@/api/types'
import { skillName } from '@/api/wiki'
import { keptFor } from '../skills'

// The skill library as a market of skills (docs/webui.md 4.10): a list of
// skills to find and install, another of patterns, and a page for each.

// LibraryRoute is what the part of the address after /library names.
export type LibraryRoute = { kind: 'skills' } | { kind: 'patterns' } | { kind: 'changes' } | { kind: 'old' } | { kind: 'page'; path: string }

// Pages all end in .md, so the names of the lists never meet one. The
// library's front page and graph are gone; their addresses are 'old'.
export function libraryRoute(rest: string): LibraryRoute {
  const trimmed = rest.replace(/^\/+|\/+$/g, '')
  if (trimmed === '') return { kind: 'skills' }
  if (trimmed === 'patterns') return { kind: 'patterns' }
  if (trimmed === 'changes') return { kind: 'changes' }
  if (trimmed.endsWith('.md')) return { kind: 'page', path: `/${trimmed}` }
  return { kind: 'old' }
}

// SkillRow is one skill as the list shows it.
export interface SkillRow {
  name: string
  path: string
  title: string
  description: string
  // The project owning it, by its wiki folder name; '' for nobody.
  team: string
  // The runtimes its tags keep it for; none means all.
  runtimes: string[]
  status: WikiStatus
  retired: boolean
  onTrial: boolean
  // Tags, for the install menu to judge each agent's runtime by.
  tags: string[]
  // The agents it is installed for.
  installed: Agent[]
}

// skillRows lists the library's skills by title, each with the agents it
// is installed for, as the agents themselves say.
export function skillRows(catalog: WikiCatalog | undefined, agents: Agent[]): SkillRow[] {
  const rows: SkillRow[] = []
  for (const page of catalog?.pages ?? []) {
    const name = skillName(page.path)
    if (page.type !== 'Skill' || !name) continue
    rows.push({
      name,
      path: page.path,
      title: page.title,
      description: page.description ?? '',
      team: page.team ?? '',
      runtimes: keptFor(page),
      status: page.status,
      retired: page.status === 'deprecated',
      onTrial: page.on_trial === true,
      tags: page.tags,
      installed: agents.filter((agent) => agent.skills.includes(name)),
    })
  }
  return rows.sort((a, b) => a.title.localeCompare(b.title))
}

// noTeam stands for "nobody" in the team filter, which no wiki folder can
// be named.
export const noTeam = '/'

// SkillFilter narrows the list: words in the name, title or description,
// and one team ('' for any, noTeam for nobody).
export interface SkillFilter {
  query: string
  team: string
}

export function filterSkills(rows: SkillRow[], { query, team }: SkillFilter): SkillRow[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean)
  return rows.filter((row) => {
    if (team === noTeam ? row.team !== '' : team !== '' && row.team !== team) return false
    const text = `${row.name} ${row.title} ${row.description}`.toLowerCase()
    return words.every((word) => text.includes(word))
  })
}

// patternPages lists the library's patterns by title.
export function patternPages(catalog: WikiCatalog | undefined): WikiPageInfo[] {
  return (catalog?.pages ?? []).filter((page) => page.type === 'Pattern').sort((a, b) => a.title.localeCompare(b.title))
}

export function filterPages(pages: WikiPageInfo[], query: string): WikiPageInfo[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean)
  return pages.filter((page) => {
    const text = `${page.title} ${page.description ?? ''}`.toLowerCase()
    return words.every((word) => text.includes(word))
  })
}

// skillFiles are the pages a skill's folder holds besides its SKILL.md: the
// references it came with, by where they are in the folder.
export function skillFiles(catalog: WikiCatalog | undefined, name: string): WikiPageInfo[] {
  const folder = `/skills/${name}/`
  return (catalog?.pages ?? []).filter((page) => page.path.startsWith(folder) && skillName(page.path) === '').sort((a, b) => a.path.localeCompare(b.path))
}

// skillOf is the skill whose folder a page of the library is in, '' for a
// page in none.
export function skillOf(path: string): string {
  const match = /^\/skills\/([^/]+)\//.exec(path)
  return match ? match[1] : ''
}

// Back is where a page of the library leads back to: the list it is on, or
// for a skill's reference, the skill.
export type Back = { to: 'skills' } | { to: 'patterns' } | { to: 'skill'; name: string }

export function backOf(path: string): Back {
  if (path.startsWith('/patterns/')) return { to: 'patterns' }
  const name = skillOf(path)
  return name && skillName(path) === '' ? { to: 'skill', name } : { to: 'skills' }
}
