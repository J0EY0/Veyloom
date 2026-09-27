import { useQuery } from '@tanstack/react-query'
import { api } from './client'

// Veyloom's own skills (docs/design.md 5.23.6): every agent has them, in
// every project, apart from those people install for agents from the skill
// library. They come with the hub, and change only with it.

export interface BuiltinSkill {
  name: string
  // What it is for, which is what a runtime loads it by.
  description: string
}

export const builtinSkillKeys = {
  all: ['builtinSkills'] as const,
}

export function useBuiltinSkills() {
  return useQuery({
    queryKey: builtinSkillKeys.all,
    queryFn: async () => (await api.get<{ skills: BuiltinSkill[] }>('/skills/builtin')).skills,
    staleTime: Infinity,
  })
}
