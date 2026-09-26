import { CircleCheckIcon, EllipsisIcon, Undo2Icon } from 'lucide-react'
import type { ReactNode } from 'react'
import { toast } from 'sonner'
import { useSyncMember } from '@/api/branches'
import { errorText } from '@/api/errorText'
import type { MemberBranch } from '@/api/types'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Item, ItemActions, ItemContent, ItemDescription, ItemTitle } from '@/components/ui/item'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { useT } from '@/lib/i18n'
import type { MemberOverlap } from './overlaps'
import { UnfinishedMerge } from './UnfinishedMerge'

export interface MemberBranchRowProps {
  roomId: string
  // The main line's branch.
  branch: string
  member: MemberBranch
  // The files it changed that other members changed too.
  overlaps: MemberOverlap[]
  // The members whose work its branch has in full, by name.
  holds?: string[]
  onDiff: () => void
  onMerge: () => void
  // Give up what its worktree has, once a person says so.
  onSetAside: () => void
  // Sync ran into conflicts, for the view to hand over.
  onConflicts: (files: string[]) => void
}

// overlapFilesShown caps the files an overlap line names; commitsShown the
// commits named, the latest.
const overlapFilesShown = 3
const commitsShown = 3

// MemberBranchRow is one member's worktree against the main line: its
// branch, how far ahead and behind, what it changed and which of that
// other members changed too, and what a person does to it. Nothing is done
// while a turn of the member's is under way.
export function MemberBranchRow({ roomId, branch, member, overlaps, holds = [], onDiff, onMerge, onSetAside, onConflicts }: MemberBranchRowProps) {
  const t = useT()
  const sync = useSyncMember()
  const st = member.status
  const changed = st?.files?.length ?? 0
  const added = (st?.files ?? []).reduce((sum, file) => sum + file.added, 0)
  const deleted = (st?.files ?? []).reduce((sum, file) => sum + file.deleted, 0)
  const commits = st?.commits ?? []

  function bringIn() {
    sync.mutate(member.member_id, {
      onSuccess: (result) => {
        if (result.conflicts && result.conflicts.length > 0) onConflicts(result.conflicts)
        else toast.success(t(result.updated ? 'branches.synced' : 'branches.upToDate'))
      },
      onError: (err) => toast.error(errorText(err)),
    })
  }

  let line: ReactNode
  if (!member.dir) line = t('branches.noWorktree')
  else if (member.error) line = <span className="text-status-fail">{member.error}</span>
  else if (!member.prepared) line = t('branches.notReady')
  else if (st) {
    const parts = []
    if (st.ahead > 0) parts.push(t('branches.ahead', { n: st.ahead }))
    if (st.behind > 0) parts.push(t('branches.behind', { n: st.behind }))
    parts.push(
      changed > 0
        ? t('branches.files', { n: changed }) + (st.uncommitted > 0 ? t('branches.filesUncommitted', { n: st.uncommitted }) : '')
        : t('branches.clean'),
    )
    line = (
      <>
        {parts.join(' · ')}
        {st.merging ? <span className="text-status-wait"> · {t('branches.stuckMerge')}</span> : null}
      </>
    )
  }

  const act = Boolean(member.dir) && member.prepared && !member.error
  const merging = Boolean(st?.merging)
  const busyWhy = member.busy ? t('branches.busy', { name: member.name }) : undefined
  const action = (label: string, onClick: () => void, variant: 'outline' | 'ghost', enabled: boolean) => {
    const button = (
      <Button size="xs" variant={variant} disabled={!enabled || member.busy || sync.isPending} onClick={onClick}>
        {label}
      </Button>
    )
    if (!busyWhy || !enabled) return button
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <span tabIndex={0}>{button}</span>
        </TooltipTrigger>
        <TooltipContent side="bottom">{busyWhy}</TooltipContent>
      </Tooltip>
    )
  }

  return (
    <Item size="sm" className="flex-wrap gap-x-3 gap-y-2 rounded-lg px-2 py-2.5">
      <ItemContent className="min-w-0 gap-1">
        <ItemTitle className="flex w-full flex-wrap items-center gap-2 text-[0.875rem]">
          <span className="truncate">{member.name}</span>
          {member.branch ? (
            <Badge variant="outline" className="h-4.5 rounded-[5px] px-1.5 font-mono text-[0.6875rem] font-normal text-subtle" translate="no">
              {member.branch}
            </Badge>
          ) : null}
          {added + deleted > 0 ? (
            <span className="font-mono text-[0.71875rem] font-normal tabular-nums" translate="no">
              <span className="text-status-ok">+{added}</span> <span className="text-status-fail">−{deleted}</span>
            </span>
          ) : null}
        </ItemTitle>
        <ItemDescription className="text-[0.78125rem] text-subtle">{line}</ItemDescription>
        {commits.length > 0 ? (
          <ul className="flex flex-col gap-0.5 text-[0.78125rem]">
            {commits
              .slice(-commitsShown)
              .reverse()
              .map((commit) => (
                <li key={commit.hash} className="flex min-w-0 gap-2">
                  <span className="flex-none font-mono text-subtle" translate="no">
                    {commit.hash}
                  </span>
                  <span className="min-w-0 truncate text-body">{commit.subject}</span>
                </li>
              ))}
            {commits.length > commitsShown ? <li className="text-xs text-subtle">{t('branches.commitsMore', { n: commits.length - commitsShown })}</li> : null}
          </ul>
        ) : null}
        {holds.length > 0 ? (
          <ItemDescription className="flex items-center gap-1.5 text-[0.78125rem] text-muted-foreground">
            <CircleCheckIcon aria-hidden="true" className="size-3.5 flex-none text-status-ok" />
            {t('branches.holds', { names: holds.join(t('common.listSeparator')) })}
          </ItemDescription>
        ) : null}
        {merging ? (
          <ItemDescription className="text-[0.78125rem] text-status-wait">
            {st?.conflicts?.length ? (
              <>
                {t('branches.unresolved')}{' '}
                <span className="font-mono break-all" translate="no">
                  {st.conflicts.join('、')}
                </span>
              </>
            ) : (
              t('branches.settledUncommitted')
            )}
          </ItemDescription>
        ) : null}
        {overlaps.map((o) => {
          const shown = o.files.slice(0, overlapFilesShown).join('、')
          const files = o.files.length > overlapFilesShown ? t('branches.overlapMore', { files: shown, n: o.files.length }) : shown
          return (
            <ItemDescription key={o.names.join()} className="text-[0.78125rem] text-status-wait">
              {t('branches.overlapWith', { names: o.names.join('、') })}{' '}
              <span className="font-mono break-all" translate="no">
                {files}
              </span>
            </ItemDescription>
          )
        })}
      </ItemContent>
      {act ? (
        <ItemActions className="flex-wrap">
          {action(t('branches.diff'), onDiff, 'ghost', changed > 0)}
          {merging ? (
            // Until the merge is settled or given up, nothing goes onto the
            // main line and nothing comes in.
            member.busy ? null : (
              <UnfinishedMerge roomId={roomId} member={member} branch={branch} />
            )
          ) : (
            <>
              {action(t('branches.sync'), bringIn, 'ghost', (st?.behind ?? 0) > 0)}
              {action(t('branches.merge'), onMerge, 'outline', changed > 0)}
            </>
          )}
          <DropdownMenu>
            <Tooltip>
              <TooltipTrigger asChild>
                <span tabIndex={member.busy ? 0 : -1}>
                  <DropdownMenuTrigger asChild>
                    <Button size="icon-xs" variant="ghost" aria-label={t('branches.more')} disabled={member.busy} className="text-subtle">
                      <EllipsisIcon />
                    </Button>
                  </DropdownMenuTrigger>
                </span>
              </TooltipTrigger>
              <TooltipContent side="bottom">{busyWhy ?? t('branches.more')}</TooltipContent>
            </Tooltip>
            <DropdownMenuContent align="end">
              <DropdownMenuItem variant="destructive" disabled={changed === 0 && (st?.ahead ?? 0) === 0} onSelect={onSetAside}>
                <Undo2Icon />
                {t('branches.setAside')}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </ItemActions>
      ) : null}
    </Item>
  )
}
