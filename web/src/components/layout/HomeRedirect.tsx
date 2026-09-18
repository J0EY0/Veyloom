import { Navigate } from 'react-router'
import { useProjects } from '@/api/projects'
import { lastChat } from '@/lib/lastChat'

// `/` has no page of its own: every project is in the sidebar already. It
// reopens the chat opened last, or, with none remembered or its project
// gone, what waits for you.
export function HomeRedirect() {
  const projects = useProjects()
  const roomId = lastChat()
  if (roomId !== '' && projects.isPending) return null
  const known = roomId !== '' && (projects.data ?? []).some((project) => project.main_room_id === roomId)
  return <Navigate to={known ? `/rooms/${roomId}` : '/inbox'} replace />
}
