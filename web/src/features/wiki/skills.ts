import type { WikiCatalog, WikiPageInfo } from '@/api/types'
import { skillName } from '@/api/wiki'

// The library's skills as agents have them (docs/design.md 5.15): a person
// installs one for an agent, and the agent's turns are given it when its
// runtime is one the skill is for (internal/wiki/skills.go).

const runtimeTag = 'runtime-'

// forRuntime reports whether a runtime may load the skill: every runtime,
// unless runtime-<name> tags keep it for the ones they name.
export function forRuntime(page: Pick<WikiPageInfo, 'tags'>, runtime: string): boolean {
  const kept = page.tags.map((tag) => tag.toLowerCase()).filter((tag) => tag.startsWith(runtimeTag))
  return kept.length === 0 || kept.includes(runtimeTag + runtime)
}

// keptFor is the runtimes a skill's tags keep it for; none means all.
export function keptFor(page: Pick<WikiPageInfo, 'tags'>): string[] {
  return page.tags.map((tag) => tag.toLowerCase()).flatMap((tag) => (tag.startsWith(runtimeTag) ? [tag.slice(runtimeTag.length)] : []))
}

// SkillChoice is one skill an agent may have.
export interface SkillChoice {
  name: string
  title: string
  description: string
  // A draft is not given out until it is stable.
  draft: boolean
  // The runtimes its tags keep it for; none means all.
  runtimes: string[]
  // Why one the agent has is given to it no longer: gone from the library,
  // retired, or kept for other runtimes. It can still be taken off.
  problem?: 'gone' | 'retired' | 'otherRuntime'
}

// skillChoices lists the skills an agent on runtime may have, by title:
// the library's current ones for that runtime, and those it has already.
export function skillChoices(catalog: WikiCatalog | undefined, runtime: string, installed: string[]): SkillChoice[] {
  const pages = new Map<string, WikiPageInfo>()
  for (const page of catalog?.pages ?? []) {
    const name = page.type === 'Skill' ? skillName(page.path) : ''
    if (name !== '') pages.set(name, page)
  }
  const out: SkillChoice[] = []
  for (const [name, page] of pages) {
    const has = installed.includes(name)
    const retired = page.status === 'deprecated'
    const elsewhere = runtime !== '' && !forRuntime(page, runtime)
    if (!has && (retired || elsewhere)) continue
    out.push({
      name,
      title: page.title || name,
      description: page.description ?? '',
      draft: page.status === 'draft',
      runtimes: keptFor(page),
      problem: retired ? 'retired' : elsewhere ? 'otherRuntime' : undefined,
    })
  }
  for (const name of installed) {
    if (!pages.has(name)) out.push({ name, title: name, description: '', draft: false, runtimes: [], problem: 'gone' })
  }
  return out.sort((a, b) => a.title.localeCompare(b.title) || a.name.localeCompare(b.name))
}
