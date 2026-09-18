import { useQuery } from '@tanstack/react-query'
import { api } from './client'
import type { UsersResponse } from './types'

export const userKeys = {
  all: ['users'] as const,
}

export function useUsers() {
  return useQuery({
    queryKey: userKeys.all,
    queryFn: async () => (await api.get<UsersResponse>('/users')).users,
    // Users change rarely; sender names should not refetch on every focus.
    staleTime: 5 * 60_000,
  })
}
