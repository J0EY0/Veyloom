import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './client'
import type { MemoryPrefs } from './types'

// The account's settings the hub acts on (docs/design.md 5.19): which
// memories the members' turns use.

export const settingsKeys = {
  memory: ['settings', 'memory'] as const,
}

export function useMemoryPrefs() {
  return useQuery({
    queryKey: settingsKeys.memory,
    queryFn: async () => (await api.get<{ memory: MemoryPrefs }>('/settings/memory')).memory,
  })
}

// useSetMemoryPrefs saves the switches as a whole, showing them switched at
// once and back again if the save fails.
export function useSetMemoryPrefs() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (prefs: MemoryPrefs) => (await api.put<{ memory: MemoryPrefs }>('/settings/memory', prefs)).memory,
    onMutate: async (prefs) => {
      await client.cancelQueries({ queryKey: settingsKeys.memory })
      const before = client.getQueryData<MemoryPrefs>(settingsKeys.memory)
      client.setQueryData(settingsKeys.memory, prefs)
      return { before }
    },
    onError: (_err, _prefs, context) => client.setQueryData(settingsKeys.memory, context?.before),
    onSuccess: (prefs) => client.setQueryData(settingsKeys.memory, prefs),
  })
}
