import { describe, expect, it } from 'vitest'
import type { WikiCatalog, WikiPageInfo } from '@/api/types'
import { backOf, filterSkills, libraryRoute, noTeam, skillFiles, skillRows } from './model'

function info(path: string, type: string, title: string, overrides: Partial<WikiPageInfo> = {}): WikiPageInfo {
  return { path, type, title, tags: [], status: 'stable', tier: 'unverified', modified: '2026-09-23T01:00:00Z', resident: false, ...overrides }
}

const catalog: WikiCatalog = {
  pages: [
    info('/skills/b-skill/SKILL.md', 'Skill', 'Beta', { team: 'veyloom', description: 'Writes release notes' }),
    info('/skills/b-skill/references/one.md', 'Reference', 'One'),
    info('/skills/a-skill/SKILL.md', 'Skill', 'Alpha', { tags: ['runtime-pi'], on_trial: true }),
    info('/patterns/p.md', 'Pattern', 'A pattern'),
  ],
  dirs: [],
  folder: '/w',
  history: true,
}

describe('the library as a market', () => {
  it('names its views and pages, and sends the old ones home', () => {
    expect(libraryRoute('')).toEqual({ kind: 'skills' })
    expect(libraryRoute('/patterns/')).toEqual({ kind: 'patterns' })
    expect(libraryRoute('changes')).toEqual({ kind: 'changes' })
    expect(libraryRoute('skills/a/SKILL.md')).toEqual({ kind: 'page', path: '/skills/a/SKILL.md' })
    expect(libraryRoute('overview')).toEqual({ kind: 'old' })
    expect(libraryRoute('graph')).toEqual({ kind: 'old' })
  })

  it('lists skills alone, by title, installed as the agents say', () => {
    const rows = skillRows(catalog, [
      { id: 'a1', skills: ['a-skill'] },
      { id: 'a2', skills: [] },
    ] as never)
    expect(rows.map((row) => [row.name, row.title, row.runtimes, row.onTrial, row.installed.length])).toEqual([
      ['a-skill', 'Alpha', ['pi'], true, 1],
      ['b-skill', 'Beta', [], false, 0],
    ])
  })

  it('narrows by words in the name, title or description, and by team', () => {
    const rows = skillRows(catalog, [])
    expect(filterSkills(rows, { query: 'release', team: '' }).map((row) => row.name)).toEqual(['b-skill'])
    expect(filterSkills(rows, { query: 'a-skill', team: '' }).map((row) => row.name)).toEqual(['a-skill'])
    expect(filterSkills(rows, { query: '', team: 'veyloom' }).map((row) => row.name)).toEqual(['b-skill'])
    expect(filterSkills(rows, { query: '', team: noTeam }).map((row) => row.name)).toEqual(['a-skill'])
  })

  it('finds the files a skill came with, and where each page leads back', () => {
    expect(skillFiles(catalog, 'b-skill').map((page) => page.path)).toEqual(['/skills/b-skill/references/one.md'])
    expect(skillFiles(catalog, 'a-skill')).toEqual([])
    expect(backOf('/skills/b-skill/SKILL.md')).toEqual({ to: 'skills' })
    expect(backOf('/skills/b-skill/references/one.md')).toEqual({ to: 'skill', name: 'b-skill' })
    expect(backOf('/patterns/p.md')).toEqual({ to: 'patterns' })
  })
})
