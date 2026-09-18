import { useLocation, useNavigate } from 'react-router'
import { ApiError } from '@/api/client'
import { useDeleteProject } from '@/api/projects'
import type { Project } from '@/api/types'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { useT } from '@/lib/i18n'

export interface DeleteProjectDialogProps {
  project: Project
  onClose: () => void
}

// Asks before deleting a project, which deletes its whole chat: the title
// says so. It stays open until the hub answers, so a refusal is read here:
// a member still working in the project keeps it. Deleting the chat on
// screen goes home.
export function DeleteProjectDialog({ project, onClose }: DeleteProjectDialogProps) {
  const remove = useDeleteProject()
  const navigate = useNavigate()
  const { pathname } = useLocation()
  const t = useT()
  const busy = remove.error instanceof ApiError && remove.error.status === 409

  return (
    <AlertDialog open onOpenChange={(next) => (next ? undefined : onClose())}>
      <AlertDialogContent size="sm" aria-describedby={undefined}>
        <AlertDialogHeader>
          <AlertDialogTitle className="text-base">{t('project.deleteTitle', { name: project.name })}</AlertDialogTitle>
        </AlertDialogHeader>
        {remove.error ? (
          <p role="alert" className="text-center text-[0.8125rem] leading-relaxed text-status-fail">
            {busy ? t('project.deleteBusy') : remove.error.message}
          </p>
        ) : null}
        <AlertDialogFooter>
          <AlertDialogCancel>{t('common.cancel')}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={remove.isPending}
            onClick={(event) => {
              // The action would close the dialog at once; wait for the hub.
              event.preventDefault()
              remove.mutate(project, {
                onSuccess: () => {
                  onClose()
                  if (pathname.startsWith(`/rooms/${project.main_room_id}`)) void navigate('/')
                },
              })
            }}
          >
            {remove.isPending ? t('common.deleting') : t('common.delete')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
