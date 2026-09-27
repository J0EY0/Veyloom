import { queryOptions, useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { api } from './client'
import { projectKeys } from './projects'
import { refresh } from './refresh'
import type { BranchesResponse, CommitResponse, DiffResponse, MergeResponse, SetAsideResponse, SyncResponse } from './types'

// A project's branches (docs/design.md 5.21): how the main line and each
// member's worktree stand, and what a person does to them.

export const branchKeys = {
  all: ['branches'] as const,
  project: (projectId: string) => ['branches', projectId] as const,
  diff: (memberId: string) => ['branches', 'diff', memberId] as const,
  checkoutDiff: (projectId: string) => ['branches', 'checkout-diff', projectId] as const,
}

// branchesQuery reads a project's branches: the machine answers with
// git's view, which is not free, so nothing polls it.
export function branchesQuery(projectId: string) {
  return queryOptions({
    queryKey: branchKeys.project(projectId),
    queryFn: async () => (await api.get<BranchesResponse>(`/projects/${projectId}/branches`)).branches,
  })
}

// useBranches reads the branches when the tab opens and again as turns end:
// what the members did changes them.
export function useBranches(projectId: string) {
  return useQuery({ ...branchesQuery(projectId), enabled: projectId !== '' })
}

// useMemberDiff is the patch of a member's worktree, read while open.
export function useMemberDiff(memberId: string, open: boolean) {
  return useQuery({
    queryKey: branchKeys.diff(memberId),
    queryFn: () => api.get<DiffResponse>(`/members/${memberId}/diff`),
    enabled: open && memberId !== '',
    staleTime: 0,
  })
}

// useCheckoutDiff is the patch of what the project's checkout changed and
// did not commit, read while open.
export function useCheckoutDiff(projectId: string, open: boolean) {
  return useQuery({
    queryKey: branchKeys.checkoutDiff(projectId),
    queryFn: () => api.get<DiffResponse>(`/projects/${projectId}/checkout/diff`),
    enabled: open && projectId !== '',
    staleTime: 0,
  })
}

// useCommitCheckout commits the changes to the files a person picked in the
// project's checkout, with their message: the new commit.
export function useCommitCheckout(projectId: string) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async ({ message, paths }: { message: string; paths: string[] }) =>
      (await api.post<CommitResponse>(`/projects/${projectId}/checkout/commit`, { message, paths })).commit,
    onSettled: () => refreshBranches(client),
  })
}

// refreshBranches has the branches read again: a turn ended, or a person
// merged, synced or changed the steps.
export function refreshBranches(client: QueryClient) {
  refresh(client, { queryKey: branchKeys.all })
}

// useMergeMember puts a member's work on the main line with a person's
// message, leaving out the new files named.
export function useMergeMember() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async ({ memberId, message, leave }: { memberId: string; message: string; leave: string[] }) =>
      (await api.post<MergeResponse>(`/members/${memberId}/merge`, { message, leave })).merge,
    onSettled: () => refreshBranches(client),
  })
}

// useSetAside gives up what a member's worktree has, kept in git: the ref
// it is kept under.
export function useSetAside() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (memberId: string) => (await api.post<SetAsideResponse>(`/members/${memberId}/set-aside`, {})).ref,
    onSettled: () => refreshBranches(client),
  })
}

export function useSyncMember() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (memberId: string) => (await api.post<SyncResponse>(`/members/${memberId}/sync`, {})).sync,
    onSettled: () => refreshBranches(client),
  })
}

// useAbortMerge gives up a merge a member left under way in its worktree.
export function useAbortMerge() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (memberId: string) => api.post<void>(`/members/${memberId}/merge/abort`, {}),
    onSettled: () => refreshBranches(client),
  })
}

// useStartSetup has the project's leader set it up again.
export function useStartSetup(projectId: string) {
  return useMutation({
    mutationFn: () => api.post<void>(`/projects/${projectId}/setup`, {}),
  })
}

// useSettleSteps adopts the steps the leader wrote down, or turns them
// down; the project is read again either way.
export function useSettleSteps(projectId: string) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (adopt: boolean) => api.post<void>(`/projects/${projectId}/workspace/pending`, { adopt }),
    onSettled: () => {
      void client.invalidateQueries({ queryKey: projectKeys.all })
      refreshBranches(client)
    },
  })
}
