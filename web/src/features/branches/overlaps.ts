import type { Overlap } from '@/api/types'

// MemberOverlap is the files a member changed that the members named
// changed too, each on its own branch (docs/design.md 5.21).
export interface MemberOverlap {
  names: string[]
  files: string[]
}

// overlapsOf is a member's overlaps, one for each set of members it shares
// files with.
export function overlapsOf(memberId: string, overlaps: Overlap[], names: Map<string, string>): MemberOverlap[] {
  const groups = new Map<string, MemberOverlap>()
  for (const o of overlaps) {
    if (!o.members.includes(memberId)) continue
    const others = o.members.filter((id) => id !== memberId)
    const key = others.join(',')
    let group = groups.get(key)
    if (!group) {
      group = { names: others.map((id) => names.get(id) ?? id), files: [] }
      groups.set(key, group)
    }
    group.files.push(o.path)
  }
  return [...groups.values()]
}
