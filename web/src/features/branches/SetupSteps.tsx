import { toast } from 'sonner'
import { useSettleSteps, useStartSetup } from '@/api/branches'
import { errorText } from '@/api/errorText'
import type { Project, WorkspaceSteps } from '@/api/types'
import { Button } from '@/components/ui/button'
import { useT } from '@/lib/i18n'

// StepsText says what getting a new worktree ready does: the paths copied
// from the checkout, then the command, set in code.
export function StepsText({ steps }: { steps: WorkspaceSteps }) {
  const t = useT()
  if (steps.copy.length === 0 && steps.run === '') return <>{t('branches.setup.none')}</>
  return (
    <>
      {steps.copy.length > 0 ? (
        <>
          {t('branches.setup.copy')}{' '}
          <span className="font-mono text-[0.78125rem]" translate="no">
            {steps.copy.join('、')}
          </span>
        </>
      ) : null}
      {steps.copy.length > 0 && steps.run !== '' ? <span aria-hidden="true"> · </span> : null}
      {steps.run !== '' ? (
        <>
          {t('branches.setup.run')}{' '}
          <code className="font-mono text-[0.78125rem] break-all" translate="no">
            {steps.run}
          </code>
        </>
      ) : null}
    </>
  )
}

export interface SetupStepsProps {
  project: Project
  onEdit: () => void
}

// SetupSteps is how the project stands for its members' worktrees
// (docs/design.md 5.21): steps the leader wrote down that wait for a
// person, the steps new worktrees get, or that nobody set it up yet; and
// what a person does about it.
export function SetupSteps({ project, onEdit }: SetupStepsProps) {
  const t = useT()
  const start = useStartSetup(project.id)

  function setUpAgain() {
    start.mutate(undefined, {
      onSuccess: () => toast.success(t('branches.setup.started')),
      onError: (err) => toast.error(errorText(err)),
    })
  }

  if (project.workspace_pending) {
    return <PendingSteps project={project} />
  }
  const steps = { copy: project.workspace_copy ?? [], run: project.workspace_run ?? '' }
  return (
    <div className="flex flex-col gap-2">
      <p className="text-[0.8125rem] text-muted-foreground">
        {project.initialized_at ? (
          <>
            <span className="text-foreground">{t('branches.setup.title')}</span>
            <span aria-hidden="true">：</span>
            <StepsText steps={steps} />
          </>
        ) : (
          t('branches.setup.notYet')
        )}
      </p>
      <div className="flex gap-2">
        <Button size="xs" variant="outline" onClick={onEdit}>
          {t('branches.setup.edit')}
        </Button>
        <Button size="xs" variant="ghost" className="text-muted-foreground" disabled={start.isPending} onClick={setUpAgain}>
          {project.initialized_at ? t('branches.setup.again') : t('branches.setup.now')}
        </Button>
      </div>
    </div>
  )
}

// PendingSteps are the steps the leader wrote down with a command, waiting
// for a person to adopt them or turn them down (docs/design.md 5.21): on
// the Branches tab, and as the card in the setup topic.
export function PendingSteps({ project }: { project: Project }) {
  const t = useT()
  const settle = useSettleSteps(project.id)
  if (!project.workspace_pending) return null

  function decide(adopt: boolean) {
    settle.mutate(adopt, {
      onSuccess: () => toast.success(t(adopt ? 'branches.setup.adopted' : 'branches.setup.dropped')),
      onError: (err) => toast.error(errorText(err)),
    })
  }

  return (
    <div className="flex flex-col gap-2 rounded-[10px] bg-muted px-3.5 py-3">
      <p className="text-[0.8125rem] font-medium">{t('branches.setup.pending')}</p>
      <p className="text-[0.8125rem] text-muted-foreground">
        <StepsText steps={project.workspace_pending} />
      </p>
      <div className="flex gap-2">
        <Button size="xs" disabled={settle.isPending} onClick={() => decide(true)}>
          {t('branches.setup.adopt')}
        </Button>
        <Button size="xs" variant="ghost" className="text-muted-foreground" disabled={settle.isPending} onClick={() => decide(false)}>
          {t('branches.setup.drop')}
        </Button>
      </div>
    </div>
  )
}
