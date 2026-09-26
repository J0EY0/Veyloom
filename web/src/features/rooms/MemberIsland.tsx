import { useDecideApproval } from '@/api/approvals'
import { AgentAvatar } from '@/components/shared/agent-avatar'
import { StatusDot } from '@/components/shared/status-dot'
import { UserAvatar } from '@/components/shared/user-avatar'
import { Button } from '@/components/ui/button'
import { HoverCard, HoverCardContent, HoverCardTrigger } from '@/components/ui/hover-card'
import { Item, ItemActions, ItemContent, ItemGroup, ItemMedia, ItemTitle } from '@/components/ui/item'
import { Separator } from '@/components/ui/separator'
import { approvalCommand } from '@/features/approvals/describe'
import { askKind, waitingKeys } from '@/features/approvals/kinds'
import { useCurrentUser } from '@/lib/currentUser'
import { formatElapsed } from '@/lib/format'
import { useLiveTurn } from '@/lib/liveTurns'
import { useNow } from '@/lib/useNow'
import { cn } from '@/lib/utils'
import { statusLabelKey, statusTone } from './memberStatus'
import type { MemberState } from './useMemberStates'
import { useT } from '@/lib/i18n'
import { toast } from 'sonner'
import { errorText } from '@/api/errorText'

export interface MemberIslandProps {
  states: MemberState[]
  onOpenThread: (threadId: string) => void
  onOpenMembers: () => void
}

// The capsule in the chat's top bar (docs/webui.md §4.4): one avatar per
// member with a ring for state, and to its right the one thing worth
// saying, in the wait colour when a person is waited for. Hovering the
// avatars lists every member; clicking a working one opens the topic it is
// working in.
export function MemberIsland({ states, onOpenThread, onOpenMembers }: MemberIslandProps) {
  const waiting = states.find((s) => s.status === 'waiting')
  const working = states.filter((s) => s.status === 'working')
  const lead = working[0]
  const now = useNow(lead !== undefined && waiting === undefined)
  const live = useLiveTurn(lead?.turn?.id)
  const user = useCurrentUser()
  const decide = useDecideApproval()
  const t = useT()
  const decideFailed = (err: Error) => toast.error(errorText(err, { 409: t('approval.raced') }))

  return (
    <div
      role="status"
      aria-label={t('island.label')}
      className={cn(
        'flex h-8 max-w-[min(34rem,50vw)] min-w-0 items-center gap-2.5 rounded-full border px-1 text-[0.78125rem] whitespace-nowrap',
        waiting ? 'border-status-wait/40 bg-status-wait/8' : 'border-border',
      )}
    >
      <HoverCard openDelay={200} closeDelay={100}>
        <HoverCardTrigger asChild>
          <div className="flex items-center">
            {states.map((state, index) => (
              <Face key={state.member.id} state={state} first={index === 0} onOpenThread={onOpenThread} />
            ))}
          </div>
        </HoverCardTrigger>
        <HoverCardContent align="start" sideOffset={8} className="w-72 p-1.5">
          <ItemGroup>
            {states.map((state) => (
              <MemberRow key={state.member.id} state={state} onOpenThread={onOpenThread} />
            ))}
          </ItemGroup>
        </HoverCardContent>
      </HoverCard>
      <Separator orientation="vertical" className="h-4.5! bg-input" />
      {waiting?.approval && askKind(waiting.approval.kind) !== 'tool_use' ? (
        // A question, a form or a link is dealt with on its card, in its
        // topic, not from here.
        <>
          <span className="min-w-0 truncate text-muted-foreground">
            <b className="font-medium text-foreground">{waiting.member.display_name}</b> {t(waitingKeys[askKind(waiting.approval.kind)].island)} ·{' '}
            {approvalCommand(waiting.approval)}
          </span>
          <Button size="xs" className="rounded-full px-3" onClick={() => onOpenThread(waiting.approval!.thread_id)}>
            {t(askKind(waiting.approval.kind) === 'question' ? 'island.answer' : 'island.handle')}
          </Button>
        </>
      ) : waiting?.approval ? (
        <>
          <span className="min-w-0 truncate text-muted-foreground">
            <b className="font-medium text-foreground">{waiting.member.display_name}</b> {t('island.waiting')} ·{' '}
            <span className="font-mono text-[0.75rem]" translate="no">
              {approvalCommand(waiting.approval)}
            </span>
          </span>
          <Button
            size="xs"
            variant="ghost"
            disabled={!user || decide.isPending}
            onClick={() => user && decide.mutate({ id: waiting.approval!.id, user_id: user.id, allow: false, message: '' }, { onError: decideFailed })}
          >
            {t('common.deny')}
          </Button>
          <Button
            size="xs"
            className="rounded-full px-3"
            disabled={!user || decide.isPending}
            onClick={() => user && decide.mutate({ id: waiting.approval!.id, user_id: user.id, allow: true, message: '' }, { onError: decideFailed })}
          >
            {t('common.allow')}
          </Button>
        </>
      ) : lead?.turn ? (
        <span className="min-w-0 truncate pr-2 text-muted-foreground">
          <b className="font-medium text-foreground">{lead.member.display_name}</b>
          {live?.tool ? (
            <>
              {' · '}
              <span className="font-mono text-[0.75rem]" translate="no">
                {live.tool}
              </span>
            </>
          ) : (
            ` ${t('island.working')}`
          )}
          {' · '}
          <span className="tabular-nums">{formatElapsed(lead.turn.started_at, now)}</span>
          {working.length > 1 ? t('island.moreBusy', { n: working.length - 1 }) : ''}
        </span>
      ) : (
        <Button
          variant="ghost"
          size="xs"
          data-opens-panel
          onClick={onOpenMembers}
          className="mr-1 rounded-full font-normal text-subtle hover:bg-transparent hover:text-foreground"
        >
          {t('island.idle', { n: states.length })}
        </Button>
      )}
    </div>
  )
}

function Face({ state, first, onOpenThread }: { state: MemberState; first: boolean; onOpenThread: (id: string) => void }) {
  const t = useT()
  const label = `${state.member.display_name} · ${t(statusLabelKey(state))}`
  const ring =
    state.status === 'working'
      ? 'shadow-[0_0_0_2px_var(--background),0_0_0_3.5px_var(--status-run)]'
      : state.status === 'waiting'
        ? 'shadow-[0_0_0_2px_var(--background),0_0_0_3.5px_var(--status-wait)]'
        : 'shadow-[0_0_0_2px_var(--background)]'
  const dim = state.status === 'idle' ? 'opacity-45' : state.status === 'working' || state.status === 'waiting' ? '' : 'opacity-30'
  const avatar = (
    <span className={cn('relative inline-flex', !first && '-ml-1.5')}>
      {state.look ? (
        <AgentAvatar look={state.look} name={state.member.display_name} mark={false} className={cn(ring, dim)} />
      ) : (
        <UserAvatar name={state.member.display_name} className={cn(ring, dim)} />
      )}
      {state.status === 'working' ? (
        <span aria-hidden="true" className="absolute -inset-1.5 animate-ripple rounded-full border-[1.5px] border-status-run" />
      ) : null}
    </span>
  )
  if (state.turn) {
    const threadId = state.turn.thread_id
    return (
      <Button
        variant="ghost"
        size="icon"
        aria-label={t('island.openTopic', { label })}
        data-opens-panel
        onClick={() => onOpenThread(threadId)}
        className="size-auto rounded-full p-0 hover:bg-transparent"
      >
        {avatar}
      </Button>
    )
  }
  return <span aria-label={label}>{avatar}</span>
}

// One member in the hover list: name, state, and what it is doing now.
function MemberRow({ state, onOpenThread }: { state: MemberState; onOpenThread: (id: string) => void }) {
  const t = useT()
  const live = useLiveTurn(state.turn?.id)
  const detail = state.approval ? approvalCommand(state.approval) : live?.tool
  const row = (
    <>
      <ItemMedia>
        {state.look ? <AgentAvatar look={state.look} name={state.member.display_name} size="sm" /> : <UserAvatar name={state.member.display_name} size="sm" />}
      </ItemMedia>
      <ItemContent className="min-w-0">
        <ItemTitle className="w-full text-[0.8125rem] font-normal">
          <span className="truncate">{state.member.display_name}</span>
        </ItemTitle>
      </ItemContent>
      <ItemActions className="gap-2">
        {detail ? (
          <span className="max-w-28 min-w-0 truncate font-mono text-[0.71875rem] text-subtle" translate="no">
            {detail}
          </span>
        ) : null}
        <span className="inline-flex flex-none items-center gap-1.5 text-xs text-muted-foreground">
          <StatusDot tone={statusTone[state.status]} />
          {t(statusLabelKey(state))}
        </span>
      </ItemActions>
    </>
  )
  const itemClass = 'h-8 flex-nowrap gap-2 rounded-md border-0 px-2 py-0'
  if (state.turn) {
    const threadId = state.turn.thread_id
    return (
      <div role="listitem">
        <Item asChild size="sm" className={cn(itemClass, 'w-full text-left hover:bg-muted')}>
          <button type="button" data-opens-panel onClick={() => onOpenThread(threadId)}>
            {row}
          </button>
        </Item>
      </div>
    )
  }
  return (
    <Item role="listitem" size="sm" className={itemClass}>
      {row}
    </Item>
  )
}
