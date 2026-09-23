import { ApiError } from '@/api/client'
import { useDeleteAgent } from '@/api/agents'
import type { Agent } from '@/api/types'
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
import { projectsInUse } from './inUse'
import { errorText } from '@/api/errorText'

export interface DeleteAgentDialogProps {
  agent: Agent
  onClose: () => void
}

// Asks before deleting an agent. The dialog stays open until the hub
// answers, so a refusal is read here rather than lost behind a toast: an
// agent still in a project cannot go, and the hub names the projects to
// take it out of first.
export function DeleteAgentDialog({ agent, onClose }: DeleteAgentDialogProps) {
  const remove = useDeleteAgent()
  const t = useT()
  const inUse = remove.error instanceof ApiError && remove.error.status === 409
  const projects = projectsInUse(remove.error)

  return (
    <AlertDialog open onOpenChange={(next) => (next ? undefined : onClose())}>
      <AlertDialogContent size="sm" aria-describedby={undefined}>
        <AlertDialogHeader>
          <AlertDialogTitle className="text-base">{t('agents.deleteTitle', { name: agent.name })}</AlertDialogTitle>
        </AlertDialogHeader>
        {remove.error ? (
          <p role="alert" className="text-center text-[0.8125rem] leading-relaxed text-status-fail">
            {!inUse ? errorText(remove.error) : projects.length > 0 ? t('agents.deleteInProjects', { projects: projects.join('、') }) : t('agents.deleteInUse')}
          </p>
        ) : null}
        <AlertDialogFooter>
          <AlertDialogCancel>{t('common.cancel')}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={remove.isPending || inUse}
            onClick={(event) => {
              // The action would close the dialog at once; wait for the hub.
              event.preventDefault()
              remove.mutate(agent.id, { onSuccess: onClose })
            }}
          >
            {remove.isPending ? t('common.deleting') : t('common.delete')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
