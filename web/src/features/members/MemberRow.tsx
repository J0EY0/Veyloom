import { EllipsisIcon, GitMergeIcon } from 'lucide-react'
import { toast } from 'sonner'
import { useResetMemberSession, useUpdateMember } from '@/api/agents'
import { useLiftPause } from '@/api/pauses'
import { useSyncMember } from '@/api/branches'
import type { Member, MemberBranch } from '@/api/types'
import { AgentAvatar } from '@/components/shared/agent-avatar'
import { StatusDot } from '@/components/shared/status-dot'
import { UserAvatar } from '@/components/shared/user-avatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Item, ItemContent, ItemDescription, ItemMedia, ItemTitle } from '@/components/ui/item'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { approvalCommand } from '@/features/approvals/describe'
import type { BranchDialogState } from '@/features/branches/BranchDialog'
import { BranchLines } from '@/features/branches/BranchLines'
import type { MemberOverlap } from '@/features/branches/overlaps'
import { statusLabel, toneOf } from '@/features/rooms/memberStatus'
import type { MemberState } from '@/features/rooms/useMemberStates'
import { useT } from '@/lib/i18n'
import { useLiveTurn } from '@/lib/liveTurns'
import { cn } from '@/lib/utils'
import { errorText } from '@/api/errorText'

export interface MemberRowProps {
  roomId: string
  state: MemberState
  leader: boolean
  // Its worktree against the main line (docs/design.md 5.21), with the
  // files other members changed too and whose work it holds; absent for
  // a project without worktrees, and for the leader.
  branch?: MemberBranch
  overlaps?: MemberOverlap[]
  holds?: string[]
  // The leader of a project with worktrees works in the checkout itself.
  inCheckout?: boolean
  // The checkout is on no branch: nothing can be merged into it.
  detached?: boolean
  onEdit: (member: Member) => void
  onMakeLeader: (member: Member) => void
  onRemove: (member: Member) => void
  onOpenThread: (threadId: string) => void
  onBranch?: (dialog: BranchDialogState) => void
}

// One member of the chat's info panel: what it is doing right now, whether
// it leads the project, how its branch stands, and what you do to it: merge
// its work or settle a merge it left, read, sync or reset its branch; edit
// it, make it the leader, switch off, start a new session, take out.
export function MemberRow({
  roomId,
  state,
  leader,
  branch,
  overlaps = [],
  holds = [],
  inCheckout,
  detached = false,
  onEdit,
  onMakeLeader,
  onRemove,
  onOpenThread,
  onBranch,
}: MemberRowProps) {
  const t = useT()
  const update = useUpdateMember(roomId)
  const reset = useResetMemberSession()
  const sync = useSyncMember()
  const lift = useLiftPause()
  const live = useLiveTurn(state.turn?.id)
  const { member, status } = state
  const name = member.display_name
  // While the runtime compacts the session there is no tool to name, and
  // nothing else to see either: say what is going on.
  const compacting = !state.approval && live?.compacting
  const detail = state.approval ? approvalCommand(state.approval) : live?.tool
  const threadId = state.turn?.thread_id
  // A turn in flight, even one a person must approve, would go on replying
  // for a member who is gone. Status alone is not enough: a switched-off or
  // offline member can still be finishing one.
  const busy = Boolean(state.turn || state.approval)

  // What can be done to its branch: nothing while a turn of its is under
  // way, nor to a worktree not there or not ready.
  const worktree = branch && branch.dir && branch.prepared && !branch.error ? branch : undefined
  const st = worktree?.status
  const changed = st?.files?.length ?? 0
  const merging = Boolean(st?.merging)
  const branchBusy = Boolean(worktree?.busy) || busy
  const open = (kind: 'merge' | 'diff' | 'setAside' | 'settle') => onBranch?.({ kind, memberId: member.id, name })
  const action = worktree && onBranch ? (merging ? 'settle' : changed > 0 ? 'merge' : undefined) : undefined
  const unmergeable = action === 'merge' && detached

  function bringIn() {
    sync.mutate(member.id, {
      onSuccess: (result) => {
        if (result.conflicts && result.conflicts.length > 0) onBranch?.({ kind: 'conflicts', memberId: member.id, name, files: result.conflicts })
        else toast.success(t(result.updated ? 'branches.synced' : 'branches.upToDate'))
      },
      onError: (err) => toast.error(errorText(err)),
    })
  }

  return (
    // The row opens the member (a shadcn Item as a button); its buttons sit
    // beside it rather than inside, as a button cannot hold another.
    <div role="listitem" className="group/member relative">
      <Item
        asChild
        size="sm"
        className={cn('w-full flex-nowrap items-start gap-2.5 rounded-lg border-0 px-2 py-2 text-left hover:bg-muted', action ? 'pr-26' : 'pr-9')}
      >
        <button type="button" onClick={() => onEdit(member)}>
          <ItemMedia>
            {state.look ? (
              <AgentAvatar look={state.look} name={name} className={cn((status === 'offline' || status === 'disabled') && 'opacity-45')} />
            ) : (
              <UserAvatar name={name} className={cn((status === 'offline' || status === 'disabled') && 'opacity-45')} />
            )}
          </ItemMedia>
          <ItemContent className="min-w-0 gap-0.5">
            <ItemTitle className="w-full text-[0.8125rem]">
              <span className="truncate">{name}</span>
              {leader ? (
                <Badge variant="secondary" className="h-4 flex-none rounded px-1 text-[0.625rem] font-normal text-muted-foreground">
                  {t('member.leader')}
                </Badge>
              ) : null}
            </ItemTitle>
            <ItemDescription className="flex items-center gap-1.5 text-[0.71875rem] text-subtle">
              <StatusDot tone={toneOf(state)} />
              <span className="truncate">
                {statusLabel(t, state)}
                {compacting ? ` · ${t('turn.compacting')}` : detail ? ' · ' : ''}
                {detail && !compacting ? (
                  <span className="font-mono" translate="no">
                    {detail}
                  </span>
                ) : null}
                {inCheckout && !branch ? ` · ${t('branches.inCheckout')}` : ''}
              </span>
            </ItemDescription>
            {branch ? <BranchLines branch={branch} overlaps={overlaps} holds={holds} /> : null}
          </ItemContent>
        </button>
      </Item>
      <div className="absolute top-1.5 right-1 flex items-center gap-1">
        {action ? (
          <Busy why={branchBusy ? t('branches.busy', { name }) : unmergeable ? t('branches.detachedWhy') : undefined}>
            <Button size="xs" variant="outline" disabled={branchBusy || unmergeable} onClick={() => open(action)}>
              {action === 'merge' ? <GitMergeIcon aria-hidden="true" /> : null}
              {action === 'merge' ? t('tasks.merge') : t('branches.settle')}
            </Button>
          </Busy>
        ) : null}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={t('common.more')}
              // Shown on hover, and always where nothing hovers: a touch screen.
              className="text-subtle opacity-0 group-hover/member:opacity-100 focus-visible:opacity-100 data-[state=open]:opacity-100 any-pointer-coarse:opacity-100 [@media(hover:none)]:opacity-100"
            >
              <EllipsisIcon />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-44">
            {worktree && onBranch ? (
              <>
                <DropdownMenuGroup>
                  <DropdownMenuLabel className="text-xs font-normal text-subtle">{t('branches.group')}</DropdownMenuLabel>
                  <DropdownMenuItem disabled={changed === 0} onSelect={() => open('diff')}>
                    {t('branches.diff')}
                  </DropdownMenuItem>
                  <DropdownMenuItem disabled={branchBusy || merging || sync.isPending || (st?.behind ?? 0) === 0} onSelect={bringIn}>
                    {t('branches.sync')}
                  </DropdownMenuItem>
                  <DropdownMenuItem variant="destructive" disabled={branchBusy || (changed === 0 && (st?.ahead ?? 0) === 0)} onSelect={() => open('setAside')}>
                    {t('branches.setAside')}
                  </DropdownMenuItem>
                </DropdownMenuGroup>
                <DropdownMenuSeparator />
                <DropdownMenuLabel className="text-xs font-normal text-subtle">{t('branches.memberGroup')}</DropdownMenuLabel>
              </>
            ) : null}
            {state.pause ? (
              // What held it up is seen to: its turns go on (docs/design.md
              // 5.23.3).
              <DropdownMenuItem disabled={lift.isPending} onSelect={() => lift.mutate(state.pause!.id, { onError: (err) => toast.error(errorText(err)) })}>
                {t('pause.resume')}
              </DropdownMenuItem>
            ) : null}
            <DropdownMenuItem onSelect={() => onEdit(member)}>{t('common.edit')}</DropdownMenuItem>
            {!leader && member.enabled ? <DropdownMenuItem onSelect={() => onMakeLeader(member)}>{t('member.makeLeader')}</DropdownMenuItem> : null}
            {threadId ? <DropdownMenuItem onSelect={() => onOpenThread(threadId)}>{t('approvals.openTopic')}</DropdownMenuItem> : null}
            <DropdownMenuItem
              disabled={update.isPending}
              onSelect={() => update.mutate({ id: member.id, patch: { enabled: !member.enabled } }, { onError: (err) => toast.error(errorText(err)) })}
            >
              {member.enabled ? t('member.disable') : t('member.enable')}
            </DropdownMenuItem>
            {/* Sessions renew themselves; this is for the one that went wrong
                all the same. Not from under a running turn, which would go on
                in a session that is no longer the member's. */}
            <DropdownMenuItem
              disabled={busy || reset.isPending}
              onSelect={() =>
                reset.mutate(member.id, {
                  onSuccess: () => toast.success(t('member.newSessionDone', { name })),
                  onError: (err) => toast.error(errorText(err)),
                })
              }
            >
              {t('member.newSession')}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" disabled={busy} onSelect={() => onRemove(member)}>
              {t('member.remove')}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  )
}

// Busy says, over a button greyed out, that the member is at work.
function Busy({ why, children }: { why?: string; children: React.ReactNode }) {
  if (!why) return <>{children}</>
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span tabIndex={0}>{children}</span>
      </TooltipTrigger>
      <TooltipContent side="bottom">{why}</TooltipContent>
    </Tooltip>
  )
}
