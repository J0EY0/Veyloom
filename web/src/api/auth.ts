import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { setCurrentUser } from '@/lib/currentUser'
import { ApiError, api } from './client'
import type { AuthStatus, User, UserResponse } from './types'
import { userKeys } from './users'

export const authKeys = {
  status: ['auth', 'status'] as const,
}

export interface Credentials {
  name: string
  password: string
}

// Who is signed in, and whether the account still has to be created
// (docs/webui.md §4.8). The answer also drives lib/currentUser, which
// every page reads.
export function useAuthStatus() {
  return useQuery({
    queryKey: authKeys.status,
    queryFn: async () => {
      const status = await api.get<AuthStatus>('/auth/status')
      setCurrentUser(status.user ? { id: status.user.id, name: status.user.name } : null)
      return status
    },
    staleTime: Infinity,
  })
}

function useSignIn(path: '/auth/setup' | '/auth/login') {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (credentials: Credentials) => (await api.post<UserResponse>(path, credentials)).user,
    onSuccess: (user) => {
      setCurrentUser({ id: user.id, name: user.name })
      client.setQueryData<AuthStatus>(authKeys.status, { setup_required: false, user })
      // Whatever the cache still holds from before is stale now.
      void client.invalidateQueries({ predicate: (query) => query.queryKey[0] !== 'auth' })
    },
  })
}

// useSetup creates the one account and signs it in.
export function useSetup() {
  return useSignIn('/auth/setup')
}

export function useLogin() {
  return useSignIn('/auth/login')
}

export function useLogout() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async () => {
      try {
        await api.post<undefined>('/auth/logout', {})
      } catch (err) {
        // A session that ended already is as good as signed out.
        if (!(err instanceof ApiError && err.status === 401)) throw err
      }
    },
    onSuccess: () => {
      // The status flips first so the gate swaps the app for the login
      // page; clearing the cache here would detach the gate's own query.
      setCurrentUser(null)
      client.setQueryData<AuthStatus>(authKeys.status, { setup_required: false, user: null })
    },
  })
}

export function useRenameMe() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (name: string) => (await api.patch<UserResponse>('/me', { name })).user,
    onSuccess: (me) => {
      setCurrentUser({ id: me.id, name: me.name })
      client.setQueryData<AuthStatus>(authKeys.status, (old) => (old ? { ...old, user: me } : old))
      client.setQueryData<User[]>(userKeys.all, (old) => (old ? old.map((u) => (u.id === me.id ? me : u)) : old))
    },
  })
}

export function useChangePassword() {
  return useMutation({
    mutationFn: async (body: { current: string; new: string }) => {
      await api.post<undefined>('/me/password', body)
    },
  })
}
