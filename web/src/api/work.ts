import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { browserTimeZone } from '@/lib/format'
import { api } from './client'
import type { TasksResponse, UsageRange, UsageResponse, WorkResponse } from './types'

export const workKeys = {
  // Every piece of work's page: what a room's events refresh.
  all: ['works'] as const,
  one: (chain: string) => ['works', chain] as const,
  tasks: (roomId: string) => ['rooms', roomId, 'tasks'] as const,
  usage: (range: UsageRange, projectId: string) => ['usage', range, projectId] as const,
}

// The task board of a room's project (docs/webui.md 4.20). The room's
// events refresh it as turns start and end and requests are settled.
export function useRoomTasks(roomId: string) {
  return useQuery({
    queryKey: workKeys.tasks(roomId),
    queryFn: async () => (await api.get<TasksResponse>(`/rooms/${roomId}/tasks`)).tasks,
    enabled: roomId !== '',
  })
}

// A piece of work's page, by the message a person began it with.
export function useWork(chain: string) {
  return useQuery({
    queryKey: workKeys.one(chain),
    queryFn: async () => (await api.get<WorkResponse>(`/works/${chain}`)).work,
    enabled: chain !== '',
  })
}

// Where the tokens went over a range, of one project or every one, in days
// that begin at this browser's midnight. What was shown stays up while
// another range loads.
export function useUsage(range: UsageRange, projectId: string) {
  return useQuery({
    queryKey: workKeys.usage(range, projectId),
    queryFn: async () => {
      const params = new URLSearchParams({ range, tz: browserTimeZone() })
      if (projectId !== '') params.set('project', projectId)
      return (await api.get<UsageResponse>(`/usage?${params}`)).usage
    },
    placeholderData: keepPreviousData,
  })
}
