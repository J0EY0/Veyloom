import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { agentKeys, machineKeys } from './agents'
import { approvalKeys } from './approvals'
import { api, ApiError } from './client'
import { topicKeys } from './topics'
import { upkeepKeys } from './upkeep'
import type { CreateProjectRequest, Project, ProjectResponse, ProjectsResponse, UpdateProjectRequest } from './types'

export const projectKeys = {
  all: ['projects'] as const,
}

export function useProjects() {
  return useQuery({
    queryKey: projectKeys.all,
    queryFn: async () => (await api.get<ProjectsResponse>('/projects')).projects,
  })
}

// useProject finds a project in the list the sidebar already holds, so a
// chat page needs no second request for its title or its checkout.
export function useProject(projectId: string): Project | undefined {
  const projects = useProjects()
  return projects.data?.find((project) => project.id === projectId)
}

export function useProjectName(projectId: string): string | undefined {
  return useProject(projectId)?.name
}

// Creating a project also creates its group chat with the agents asked
// for as its members; the project lands in the cache so the sidebar shows
// it without a refetch, and the agents are read again, now in a project.
export function useCreateProject() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (req: CreateProjectRequest) => api.post<ProjectResponse>('/projects', req),
    onSuccess: ({ project }, req) => {
      client.setQueryData<Project[]>(projectKeys.all, (old) => (old ? [...old, project] : old))
      if (req.agent_ids.length > 0) void client.invalidateQueries({ queryKey: agentKeys.all })
    },
  })
}

// Renaming a project or moving its checkout. The project is swapped in the
// list everyone reads; what carries its name (agents, machines, approvals,
// the inbox) or its members' paths is read again.
export function useUpdateProject(projectId: string) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (req: UpdateProjectRequest) => api.patch<ProjectResponse>(`/projects/${projectId}`, req),
    onSuccess: ({ project }) => {
      client.setQueryData<Project[]>(projectKeys.all, (old) => old?.map((p) => (p.id === project.id ? project : p)))
      refreshAfterProjectChange(client)
      // The wiki maintainer may have changed.
      void client.invalidateQueries({ queryKey: upkeepKeys.project(project.id) })
    },
  })
}

// Deleting a project deletes its whole chat; one already gone counts as
// deleted. The project leaves the list and its chat's cache goes with it.
export function useDeleteProject() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (project: Project) => {
      try {
        await api.delete(`/projects/${project.id}`)
      } catch (err) {
        if (!(err instanceof ApiError && err.status === 404)) throw err
      }
      return project
    },
    onSuccess: (project) => {
      client.setQueryData<Project[]>(projectKeys.all, (old) => old?.filter((p) => p.id !== project.id))
      client.removeQueries({ queryKey: ['rooms', project.main_room_id] })
      refreshAfterProjectChange(client)
    },
  })
}

// Reads again what carries a project's name or its members: members'
// paths, agents' projects, machines' members, approvals, the inbox and the
// topics in the sidebar.
function refreshAfterProjectChange(client: QueryClient) {
  void client.invalidateQueries({ predicate: ({ queryKey }) => queryKey[0] === 'rooms' && queryKey[2] === 'members' })
  void client.invalidateQueries({ queryKey: agentKeys.all })
  void client.invalidateQueries({ queryKey: machineKeys.all })
  void client.invalidateQueries({ queryKey: approvalKeys.all })
  void client.invalidateQueries({ predicate: ({ queryKey }) => queryKey[2] === 'inbox' })
  void client.invalidateQueries({ queryKey: topicKeys.running })
}
