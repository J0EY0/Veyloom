import { useQuery } from '@tanstack/react-query'
import { api } from './client'
import type { TopicsResponse } from './types'

export const topicKeys = {
  running: ['topics', 'running'] as const,
}

// The topics agents are working on right now, across every project: the
// sidebar lists them under each project. Polled, since only the open
// chat has a live event stream; the chat's own turn events refresh it too.
export function useRunningTopics() {
  return useQuery({
    queryKey: topicKeys.running,
    queryFn: async () => (await api.get<TopicsResponse>('/topics?status=running')).topics,
    refetchInterval: 10_000,
  })
}
