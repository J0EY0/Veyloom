import { useQuery } from '@tanstack/react-query'
import { api } from './client'

// How a runtime takes its turns, as the hub's one table of it says
// (docs/design.md 5.23.9), where that matters to what the UI says.
export interface RuntimeTraits {
  // The system prompt, the role card with it, is given with every run;
  // otherwise the runtime fixes it when a session starts (Codex).
  system_prompt_each_run: boolean
  steer: boolean
  // It writes the wiki in the read-only preset too; Codex does not.
  wiki_when_read_only: boolean
}

// useRuntimeTraits reads the table once: it changes with the hub alone.
export function useRuntimeTraits() {
  return useQuery({
    queryKey: ['runtime-traits'],
    queryFn: async () => (await api.get<{ traits: Record<string, RuntimeTraits> }>('/runtime-traits')).traits,
    staleTime: Infinity,
  })
}

// noWikiReadOnly says an agent on runtime, in preset, keeps no wiki: its
// runtime writes nothing read-only it is not told to in so many words
// (docs/design.md 5.12). Until the table is read it says nothing.
export function noWikiReadOnly(traits: Record<string, RuntimeTraits> | undefined, runtime: string | undefined, preset: string | undefined): boolean {
  const known = runtime ? traits?.[runtime] : undefined
  return known !== undefined && !known.wiki_when_read_only && preset === 'read_only'
}

// fixesRoleCard says the runtime fixes the role card when a session
// starts: a new one reaches its members in a new session (docs/design.md
// 5.6). Until the table is read it says nothing.
export function fixesRoleCard(traits: Record<string, RuntimeTraits> | undefined, runtime: string): boolean {
  const known = traits?.[runtime]
  return known !== undefined && !known.system_prompt_each_run
}
