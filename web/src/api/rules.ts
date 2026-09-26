import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './client'
import type { MemberRule, MemberRulesResponse } from './types'

export const ruleKeys = {
  member: (memberId: string) => ['members', memberId, 'rules'] as const,
}

// What people allowed a member always, oldest first (docs/design.md 4.6).
// They are added by allowing a request "always", so an open list is read
// again whenever it is shown.
export function useMemberRules(memberId: string) {
  return useQuery({
    queryKey: ruleKeys.member(memberId),
    queryFn: async () => (await api.get<MemberRulesResponse>(`/members/${memberId}/rules`)).rules,
    enabled: memberId !== '',
    staleTime: 0,
  })
}

// Takes one of a member's rules back: from its next turn on, the member is
// asked again. The list drops it at once.
export function useDeleteMemberRule(memberId: string) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (rule: MemberRule) => api.delete(`/members/${memberId}/rules/${rule.id}`),
    onSuccess: (_, rule) => {
      client.setQueryData<MemberRule[]>(ruleKeys.member(memberId), (old) => old?.filter((r) => r.id !== rule.id))
    },
  })
}
