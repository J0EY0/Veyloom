import { useState } from 'react'
import { GitBranchIcon, GitMergeIcon } from 'lucide-react'
import { useBranches } from '@/api/branches'
import { errorText } from '@/api/errorText'
import { useProject } from '@/api/projects'
import type { MemberBranch } from '@/api/types'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { ItemGroup } from '@/components/ui/item'
import { Skeleton } from '@/components/ui/skeleton'
import { useT } from '@/lib/i18n'
import { CheckoutChanges, CommitDialog } from './CheckoutChanges'
import { Conflicts } from './Conflicts'
import { DiffDialog } from './DiffDialog'
import { MemberBranchRow } from './MemberBranchRow'
import { MergeDialog } from './MergeDialog'
import { overlapsOf } from './overlaps'
import { SetAsideDialog } from './SetAsideDialog'
import { SetupSteps } from './SetupSteps'
import { StepsDialog } from './StepsDialog'

export interface BranchesViewProps {
  projectId: string
  roomId: string
  onOpenThread: (threadId: string) => void
}

// BranchesView is the chat's Branches tab (docs/design.md 5.21, webui.md
// 4.16): the main line, the project's checkout on the branch it has, how
// new worktrees are got ready, and each member's worktree against the main
// line, marked with the files another member changed too.
export function BranchesView({ projectId, roomId, onOpenThread }: BranchesViewProps) {
  const t = useT()
  const project = useProject(projectId)
  const branches = useBranches(projectId)
  const [diffOf, setDiffOf] = useState<MemberBranch>()
  const [mergeOf, setMergeOf] = useState<MemberBranch>()
  const [asideOf, setAsideOf] = useState<MemberBranch>()
  // The checkout's own changes, read or being committed.
  const [checkoutDiff, setCheckoutDiff] = useState(false)
  const [committing, setCommitting] = useState(false)
  const [clash, setClash] = useState<{ member: MemberBranch; files: string[] }>()
  const [editing, setEditing] = useState(false)

  if (!project || branches.isPending) {
    return (
      <div role="status" aria-label={t('common.loading')} className="mx-auto flex w-full max-w-[46rem] flex-col gap-3 px-5 pt-6 md:px-10">
        <Skeleton className="h-6 w-40" />
        <Skeleton className="h-3 w-64" />
      </div>
    )
  }
  if (branches.isError) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>{t('branches.failed')}</EmptyTitle>
          <EmptyDescription>{errorText(branches.error)}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }
  const { main, members, overlaps = [] } = branches.data
  const names = new Map(members.map((m) => [m.member_id, m.name]))
  const branch = main.branch ?? ''
  const plain = !project.repo_path ? t('branches.noPath') : main.error ? main.error : !main.git ? t('branches.notGit') : undefined

  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <div className="mx-auto flex min-h-full w-full max-w-[46rem] flex-col gap-8 px-5 pt-5 pb-12 md:px-10">
        <header className="flex flex-col gap-1">
          <h1 className="flex items-center gap-2 text-xl font-semibold tracking-[-0.01em]">
            {t('branches.main')}
            {branch ? (
              <Badge variant="secondary" className="h-5 rounded-[5px] px-1.5 font-mono text-[0.75rem] font-normal" translate="no">
                {branch}
              </Badge>
            ) : null}
          </h1>
          {project.repo_path ? (
            <p className="font-mono text-xs break-all text-subtle" translate="no">
              {project.repo_path}
            </p>
          ) : null}
          {main.git && !branch ? <p className="text-xs text-status-wait">{t('branches.detached')}</p> : null}
        </header>

        {(main.changed?.length ?? 0) > 0 ? (
          <CheckoutChanges changed={main.changed ?? []} onDiff={() => setCheckoutDiff(true)} onCommit={() => setCommitting(true)} />
        ) : null}

        {plain !== undefined ? (
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <GitBranchIcon />
              </EmptyMedia>
              <EmptyTitle>{plain}</EmptyTitle>
            </EmptyHeader>
          </Empty>
        ) : (
          <>
            <section aria-label={t('branches.setup.title')}>
              <SetupSteps project={project} onEdit={() => setEditing(true)} />
              {project.setup_thread_id ? (
                <Button variant="link" size="xs" className="mt-1 px-0 text-muted-foreground" onClick={() => onOpenThread(project.setup_thread_id as string)}>
                  {t('branches.setup.openTopic')}
                </Button>
              ) : null}
            </section>

            {members.length === 0 ? (
              <Empty>
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <GitMergeIcon />
                  </EmptyMedia>
                  <EmptyTitle>{t('branches.noMembers')}</EmptyTitle>
                </EmptyHeader>
              </Empty>
            ) : (
              <section aria-label={t('branches.members')} className="flex flex-col gap-2">
                <h2 className="text-xs font-medium text-subtle">{t('branches.members')}</h2>
                <ItemGroup className="-mx-2">
                  {members.map((member) => (
                    <MemberBranchRow
                      key={member.member_id}
                      roomId={roomId}
                      branch={branch}
                      member={member}
                      overlaps={overlapsOf(member.member_id, overlaps, names)}
                      holds={(member.contains ?? []).flatMap((held) => names.get(held) ?? [])}
                      onDiff={() => setDiffOf(member)}
                      onMerge={() => setMergeOf(member)}
                      onSetAside={() => setAsideOf(member)}
                      onConflicts={(files) => setClash({ member, files })}
                    />
                  ))}
                </ItemGroup>
              </section>
            )}
          </>
        )}
      </div>
      {diffOf ? <DiffDialog memberId={diffOf.member_id} name={diffOf.name} onClose={() => setDiffOf(undefined)} /> : null}
      {mergeOf ? (
        <MergeDialog
          roomId={roomId}
          member={mergeOf}
          branch={branch}
          onClose={() => setMergeOf(undefined)}
          onCheckoutDiff={() => setCheckoutDiff(true)}
          onCommitCheckout={() => setCommitting(true)}
        />
      ) : null}
      {asideOf ? <SetAsideDialog member={asideOf} onClose={() => setAsideOf(undefined)} /> : null}
      {checkoutDiff ? <DiffDialog checkoutOf={projectId} onClose={() => setCheckoutDiff(false)} /> : null}
      {committing ? <CommitDialog projectId={projectId} changed={main.changed ?? []} onClose={() => setCommitting(false)} /> : null}
      {editing ? <StepsDialog project={project} onClose={() => setEditing(false)} /> : null}
      {clash ? (
        <Dialog open onOpenChange={(open) => (open ? undefined : setClash(undefined))}>
          <DialogContent aria-describedby={undefined}>
            <DialogHeader>
              <DialogTitle>{t('branches.syncConflicts')}</DialogTitle>
            </DialogHeader>
            <Conflicts
              roomId={roomId}
              memberId={clash.member.member_id}
              name={clash.member.name}
              branch={branch}
              files={clash.files}
              onDone={() => setClash(undefined)}
            />
          </DialogContent>
        </Dialog>
      ) : null}
    </div>
  )
}
