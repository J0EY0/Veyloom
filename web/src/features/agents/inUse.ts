import { ApiError } from '@/api/client'

// The projects a 409 about an agent names: where it is still a member, so
// it cannot be deleted or moved to another machine. Empty for any other
// error.
export function projectsInUse(error: unknown): string[] {
  if (!(error instanceof ApiError) || error.status !== 409) return []
  const projects = error.body.projects
  return Array.isArray(projects) ? projects.filter((name): name is string => typeof name === 'string') : []
}
