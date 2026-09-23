import { describe, expect, it } from 'vitest'
import type { WikiCatalog, WikiPageInfo } from '@/api/types'
import { skillName, skillPath } from '@/api/wiki'
import { forRuntime, keptFor, skillChoices } from './skills'

function page(path: string, overrides: Partial<WikiPageInfo> = {}): WikiPageInfo {
  return { path, type: 'Skill', title: '', tags: [], status: 'stable', tier: 'unverified', modified: '2026-09-22T00:00:00Z', resident: false, ...overrides }
}

describe('skills', () => {
  it('names a skill by its page, and its page by the name', () => {
    expect(skillName('/skills/go-table-tests/SKILL.md')).toBe('go-table-tests')
    expect(skillName('/skills/go-table-tests/references/cases.md')).toBe('')
    expect(skillName('/patterns/flaky.md')).toBe('')
    expect(skillPath('go')).toBe('/skills/go/SKILL.md')
  })

  it('keeps a skill for the runtimes its tags name, or for every one', () => {
    expect(forRuntime({ tags: [] }, 'pi')).toBe(true)
    expect(forRuntime({ tags: ['Runtime-Claude', 'tests'] }, 'claude')).toBe(true)
    expect(forRuntime({ tags: ['runtime-claude'] }, 'codex')).toBe(false)
    expect(keptFor({ tags: ['runtime-claude', 'tests', 'RUNTIME-PI'] })).toEqual(['claude', 'pi'])
  })

  it('offers the current skills for a runtime, and every one an agent has', () => {
    const catalog: WikiCatalog = {
      pages: [
        page('/skills/b/SKILL.md', { title: 'Beta', status: 'draft' }),
        page('/skills/a/SKILL.md', { title: 'Alpha', description: 'Use for a.' }),
        page('/skills/c/SKILL.md', { title: 'Claude', tags: ['runtime-claude'] }),
        page('/skills/d/SKILL.md', { title: 'Done', status: 'deprecated' }),
        page('/patterns/p.md', { type: 'Pattern', title: 'A pattern' }),
      ],
      dirs: [],
      folder: '',
      history: true,
    }
    expect(skillChoices(catalog, 'codex', []).map((c) => c.name)).toEqual(['a', 'b'])
    const all = skillChoices(catalog, 'codex', ['c', 'd', 'x'])
    expect(all.map((c) => [c.name, c.problem ?? ''])).toEqual([
      ['a', ''],
      ['b', ''],
      ['c', 'otherRuntime'],
      ['d', 'retired'],
      ['x', 'gone'],
    ])
    expect(all[0]).toMatchObject({ title: 'Alpha', description: 'Use for a.', draft: false })
    expect(all[1].draft).toBe(true)
    expect(all[2].runtimes).toEqual(['claude'])
    // No runtime picked yet: nothing is kept out for it.
    expect(skillChoices(catalog, '', []).map((c) => c.name)).toEqual(['a', 'b', 'c'])
    expect(skillChoices(undefined, 'pi', ['a']).map((c) => c.problem)).toEqual(['gone'])
  })
})
