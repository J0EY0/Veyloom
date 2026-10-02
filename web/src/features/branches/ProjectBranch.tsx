import { TriangleAlertIcon } from 'lucide-react'
import type { MainLine, Project } from '@/api/types'
import { Button } from '@/components/ui/button'
import { useT } from '@/lib/i18n'
import type { BranchDialogState } from './BranchDialog'
import { SetupSteps } from './SetupSteps'

export interface ProjectBranchProps {
  project: Project
  main: MainLine
  onBranch: (dialog: BranchDialogState) => void
  onOpenThread: (threadId: string) => void
}

// ProjectBranch is the project's side of its branches, under its card in
// the chat's info (docs/design.md 5.21): changes made in the checkout and
// not committed, which the members' worktrees lack and a merge may trip
// on; and how new worktrees are got ready. A checkout on no branch says
// so on the card, and the merges wait for one. Only a project in a git repository has worktrees at all.
export function ProjectBranch({ project, main, onBranch, onOpenThread }: ProjectBranchProps) {
  const t = useT()
  const changed = main.changed ?? []
  return (
    <div className="flex flex-col gap-3">
      {changed.length > 0 ? (
        <section
          aria-label={t('branches.uncommittedTitle', { n: changed.length })}
          className="flex items-center gap-2 rounded-lg border border-status-wait/30 bg-status-wait/8 py-1.5 pr-1.5 pl-2.5"
        >
          <TriangleAlertIcon aria-hidden="true" className="size-3.5 flex-none text-status-wait" />
          <span className="min-w-0 flex-1 text-xs text-status-wait">{t('branches.uncommittedTitle', { n: changed.length })}</span>
          <Button size="xs" variant="ghost" onClick={() => onBranch({ kind: 'checkoutDiff' })}>
            {t('branches.diff')}
          </Button>
          <Button size="xs" variant="outline" onClick={() => onBranch({ kind: 'commit' })}>
            {t('branches.commit')}
          </Button>
        </section>
      ) : null}
      <section aria-label={t('branches.setup.title')} className="flex flex-col gap-1">
        <SetupSteps project={project} onEdit={() => onBranch({ kind: 'steps' })} />
        {project.setup_thread_id ? (
          <Button variant="link" size="xs" className="self-start px-0 text-muted-foreground" onClick={() => onOpenThread(project.setup_thread_id as string)}>
            {t('branches.setup.openTopic')}
          </Button>
        ) : null}
      </section>
    </div>
  )
}
