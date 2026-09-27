import { useBranches } from '@/api/branches'
import { errorText } from '@/api/errorText'
import { useProject } from '@/api/projects'
import { useRoom } from '@/api/rooms'
import type { MemberBranch } from '@/api/types'
import { useRoomTasks } from '@/api/work'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Spinner } from '@/components/ui/spinner'
import { useT } from '@/lib/i18n'
import { CommitDialog } from './CommitDialog'
import { Conflicts } from './Conflicts'
import { DiffDialog } from './DiffDialog'
import { MergeDialog } from './MergeDialog'
import { overlapsOf } from './overlaps'
import { SetAsideDialog } from './SetAsideDialog'
import { StepsDialog } from './StepsDialog'
import { UnfinishedMerge } from './UnfinishedMerge'

// BranchDialogState is the one dialog about a project's branches open at a
// time (docs/design.md 5.21): a member's worktree read, merged, reset or
// its conflicts settled; the checkout's own changes read or committed; the
// steps new worktrees get. Whoever shows the branches holds it, so that one
// dialog can hand over to the next as it closes.
export type BranchDialogState =
  | { kind: 'merge' | 'diff' | 'setAside' | 'settle'; memberId: string; name: string }
  | { kind: 'conflicts'; memberId: string; name: string; files: string[] }
  | { kind: 'checkoutDiff' | 'commit' | 'steps' }

export interface BranchDialogProps {
  roomId: string
  state: BranchDialogState | undefined
  onChange: (next: BranchDialogState | undefined) => void
}

// BranchDialog draws the dialog the state names, from the branches as the
// hub last read them; closed, it reads nothing, so a board of cards to
// merge does not have the machine read git for each.
export function BranchDialog({ roomId, state, onChange }: BranchDialogProps) {
  if (!state) return null
  return <OpenDialog roomId={roomId} state={state} onChange={onChange} />
}

function OpenDialog({ roomId, state, onChange }: BranchDialogProps & { state: BranchDialogState }) {
  const projectId = useRoom(roomId).data?.project_id ?? ''
  const project = useProject(projectId)
  const branches = useBranches(projectId)
  const tasks = useRoomTasks(roomId)
  const close = () => onChange(undefined)
  const data = branches.data
  const branch = data?.main.branch ?? ''

  switch (state.kind) {
    case 'checkoutDiff':
      return <DiffDialog checkoutOf={projectId} onClose={close} />
    case 'commit':
      return <CommitDialog projectId={projectId} changed={data?.main.changed ?? []} onClose={close} />
    case 'steps':
      return project ? <StepsDialog project={project} onClose={close} /> : null
    case 'diff':
      return <DiffDialog memberId={state.memberId} name={state.name} onClose={close} />
  }

  const member = data?.members.find((m) => m.member_id === state.memberId)
  if (!member) {
    return <Waiting title={state.name} error={branches.isError ? errorText(branches.error) : data ? 'missing' : undefined} onClose={close} />
  }
  switch (state.kind) {
    case 'merge': {
      const names = new Map(data?.members.map((m) => [m.member_id, m.name]))
      return (
        <MergeDialog
          roomId={roomId}
          member={member}
          branch={branch}
          works={(tasks.data ?? []).filter((task) => task.member_id === member.member_id && task.state === 'merge')}
          overlaps={overlapsOf(member.member_id, data?.overlaps ?? [], names)}
          onClose={close}
          onCheckoutDiff={() => onChange({ kind: 'checkoutDiff' })}
          onCommitCheckout={() => onChange({ kind: 'commit' })}
        />
      )
    }
    case 'setAside':
      return <SetAsideDialog member={member} onClose={close} />
    case 'settle':
      return <SettleDialog roomId={roomId} member={member} branch={branch} onClose={close} />
    case 'conflicts':
      return (
        <ConflictsDialog title={state.name} onClose={close}>
          <Conflicts roomId={roomId} memberId={member.member_id} name={member.name} branch={branch} files={state.files} onDone={close} />
        </ConflictsDialog>
      )
  }
}

// Waiting stands in while the branches load, or says why a member's
// worktree cannot be found.
function Waiting({ title, error, onClose }: { title: string; error?: string; onClose: () => void }) {
  const t = useT()
  return (
    <Dialog open onOpenChange={(open) => (open ? undefined : onClose())}>
      <DialogContent aria-describedby={undefined}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        {error === undefined ? (
          <p role="status" className="flex items-center gap-2 py-2 text-[0.8125rem] text-subtle">
            <Spinner className="size-3.5" />
            {t('common.loading')}
          </p>
        ) : (
          <p className="py-2 text-[0.8125rem] text-status-fail">{error === 'missing' ? t('branches.mergeUnknown', { name: title }) : error}</p>
        )}
      </DialogContent>
    </Dialog>
  )
}

function ConflictsDialog({ title, onClose, children }: { title: string; onClose: () => void; children: React.ReactNode }) {
  const t = useT()
  return (
    <Dialog open onOpenChange={(open) => (open ? undefined : onClose())}>
      <DialogContent aria-describedby={undefined}>
        <DialogHeader>
          <DialogTitle>
            {t('branches.syncConflicts')} · {title}
          </DialogTitle>
        </DialogHeader>
        {children}
      </DialogContent>
    </Dialog>
  )
}

// SettleDialog is what a person does about a merge a member left under way:
// the files that still have conflict markers, handed back to the member or
// given up.
function SettleDialog({ roomId, member, branch, onClose }: { roomId: string; member: MemberBranch; branch: string; onClose: () => void }) {
  const t = useT()
  const files = member.status?.conflicts ?? []
  return (
    <Dialog open onOpenChange={(open) => (open ? undefined : onClose())}>
      <DialogContent aria-describedby={undefined}>
        <DialogHeader>
          <DialogTitle>{t('branches.settleTitle', { name: member.name })}</DialogTitle>
        </DialogHeader>
        {files.length > 0 ? (
          <div className="flex flex-col gap-1.5">
            <p className="text-[0.8125rem] text-status-wait">{t('branches.unresolved')}</p>
            <ul className="flex flex-col gap-0.5 font-mono text-[0.78125rem]" translate="no">
              {files.map((file) => (
                <li key={file} className="break-all">
                  {file}
                </li>
              ))}
            </ul>
          </div>
        ) : (
          <p className="text-[0.8125rem] text-status-wait">{t('branches.settledUncommitted')}</p>
        )}
        <div className="flex flex-wrap justify-end gap-2">
          <UnfinishedMerge roomId={roomId} member={member} branch={branch} onDone={onClose} />
        </div>
      </DialogContent>
    </Dialog>
  )
}
