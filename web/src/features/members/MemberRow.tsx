import { EllipsisIcon } from 'lucide-react'
import { toast } from 'sonner'
import { useResetMemberSession, useUpdateMember } from '@/api/agents'
import type { Member } from '@/api/types'
import { AgentAvatar } from '@/components/shared/agent-avatar'
import { StatusDot } from '@/components/shared/status-dot'
import { UserAvatar } from '@/components/shared/user-avatar'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Item, ItemContent, ItemDescription, ItemMedia, ItemTitle } from '@/components/ui/item'
import { approvalCommand } from '@/features/approvals/describe'
import { statusKey, statusTone } from '@/features/rooms/memberStatus'
import type { MemberState } from '@/features/rooms/useMemberStates'
import { useT } from '@/lib/i18n'
import { useLiveTurn } from '@/lib/liveTurns'
import { cn } from '@/lib/utils'
import { errorText } from '@/api/errorText'

// One member of the chat's info panel: what it is doing right now, and what
// you do to it: edit, switch off, start a new session, take out.
export function MemberRow({
  roomId,
  state,
  onEdit,
  onRemove,
  onOpenThread,
}: {
  roomId: string
  state: MemberState
  onEdit: (member: Member) => void
  onRemove: (member: Member) => void
  onOpenThread: (threadId: string) => void
}) {
  const t = useT()
  const update = useUpdateMember(roomId)
  const reset = useResetMemberSession()
  const live = useLiveTurn(state.turn?.id)
  const { member, status } = state
  // While the runtime compacts the session there is no tool to name, and
  // nothing else to see either: say what is going on.
  const compacting = !state.approval && live?.compacting
  const detail = state.approval ? approvalCommand(state.approval) : live?.tool
  const threadId = state.turn?.thread_id
  // A turn in flight, even one a person must approve, would go on replying
  // for a member who is gone. Status alone is not enough: a switched-off or
  // offline member can still be finishing one.
  const busy = Boolean(state.turn || state.approval)

  return (
    // The row opens the member (a shadcn Item as a button); "…" sits beside
    // it rather than inside, as a button cannot hold another.
    <div role="listitem" className="group/member relative">
      <Item asChild size="sm" className="w-full flex-nowrap gap-2.5 rounded-lg border-0 px-2 py-2 pr-9 text-left hover:bg-muted">
        <button type="button" onClick={() => onEdit(member)}>
          <ItemMedia>
            {state.look ? (
              <AgentAvatar look={state.look} className={cn((status === 'offline' || status === 'disabled') && 'opacity-45')} />
            ) : (
              <UserAvatar name={member.display_name} className={cn((status === 'offline' || status === 'disabled') && 'opacity-45')} />
            )}
          </ItemMedia>
          <ItemContent className="min-w-0 gap-0.5">
            <ItemTitle className="w-full text-[0.8125rem]">
              <span className="truncate">{member.display_name}</span>
            </ItemTitle>
            <ItemDescription className="flex items-center gap-1.5 text-[0.71875rem] text-subtle">
              <StatusDot tone={statusTone[status]} />
              <span className="truncate">
                {t(statusKey[status])}
                {compacting ? ` · ${t('turn.compacting')}` : detail ? ' · ' : ''}
                {detail && !compacting ? (
                  <span className="font-mono" translate="no">
                    {detail}
                  </span>
                ) : null}
              </span>
            </ItemDescription>
          </ItemContent>
        </button>
      </Item>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t('common.more')}
            className="absolute top-1/2 right-1 -translate-y-1/2 text-subtle opacity-0 group-hover/member:opacity-100 focus-visible:opacity-100 data-[state=open]:opacity-100"
          >
            <EllipsisIcon />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-44">
          <DropdownMenuItem onSelect={() => onEdit(member)}>{t('common.edit')}</DropdownMenuItem>
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
                onSuccess: () => toast.success(t('member.newSessionDone', { name: member.display_name })),
                onError: (err) => toast.error(errorText(err)),
              })
            }
          >
            {busy ? t('member.newSessionBusy') : t('member.newSession')}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem variant="destructive" disabled={busy} onSelect={() => onRemove(member)}>
            {busy ? t('member.removeBusy') : t('member.remove')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}
