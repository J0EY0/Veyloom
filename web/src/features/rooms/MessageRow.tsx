import { memo } from 'react'
import { BookHeartIcon, GitBranchIcon, LibraryBigIcon } from 'lucide-react'
import type { Approval, RoomMessage } from '@/api/types'
import { Shimmer } from '@/components/ai-elements/shimmer'
import { AgentAvatar } from '@/components/shared/agent-avatar'
import { MessageAttachments } from '@/features/attachments/MessageAttachments'
import { NoteLine } from '@/components/shared/note-line'
import { UserAvatar } from '@/components/shared/user-avatar'
import { approvalCommand } from '@/features/approvals/describe'
import { askKind, waitingKeys } from '@/features/approvals/kinds'
import { MaintainerOfferNote } from '@/features/wiki/MaintainerOfferNote'
import { systemNote } from '@/features/threads/systemNote'
import { formatTime } from '@/lib/format'
import { useLiveTurn } from '@/lib/liveTurns'
import { cn } from '@/lib/utils'
import { AgentBody } from './AgentBody'
import { Clamped } from './Clamped'
import { MessageBody } from './MessageBody'
import { TopicFooter } from './TopicFooter'
import type { Sender } from './useSenderNames'
import { t } from '@/lib/i18n'

export interface MessageRowProps {
  message: RoomMessage
  sender: Sender
  // Display names by id, for drawing the message's mentions as pills.
  names: Map<string, string>
  // The topic's request waiting for a person, if any.
  approval?: Approval
  // The message heads the project's wiki topic, or its setup topic
  // (docs/design.md 5.21).
  systemTopic?: 'wiki' | 'setup'
  // The project whose wiki maintainer this note offers (docs/design.md
  // 5.16); set on that note only, which is drawn as a card.
  offerProjectId?: string
  onOpenThread?: (threadId: string) => void
  // Said by whoever said the message before, a moment after: drawn under
  // that one's face and name rather than again.
  continued?: boolean
  // The message's topic is the one open beside the chat.
  selected?: boolean
  // Said by the project's leader.
  leader?: boolean
  // The member whose turns the topic has, when not whoever said the root.
  worker?: string
  // The message the address asked to see, lit up a moment (docs/webui.md
  // 4.21).
  lit?: boolean
}

const bodyClass = 'text-[0.90625rem] leading-[1.65] break-words text-body'

// One top-level message (docs/webui.md §4.1): face, name and time, the
// words, and under a topic root the topic's footer. A message said a moment
// after another by the same sender goes under that one's face. System notes
// are a line with their mark. Rows are memoised so a new message at the
// bottom does not re-render the fifty above it.
export const MessageRow = memo(function MessageRow(props: MessageRowProps) {
  const { message, sender, names, approval, systemTopic, offerProjectId, onOpenThread, continued, selected, leader, worker, lit } = props
  const thread = message.thread
  const runningTurnId = thread?.last_turn?.status === 'running' ? thread.last_turn.id : undefined
  const live = useLiveTurn(runningTurnId)
  if (message.sender_kind === 'system' && thread && onOpenThread) {
    // A topic the system opened, the project's wiki or setup topic: what
    // waits in it for a person is read there.
    const Icon = systemTopic === 'setup' ? GitBranchIcon : LibraryBigIcon
    return (
      <li
        data-message-id={message.id}
        className="mx-auto grid max-w-215 grid-cols-[1.75rem_minmax(0,1fr)] gap-x-3 px-6 py-[0.6875rem] [contain-intrinsic-size:auto_4rem] [content-visibility:auto]"
      >
        <span className="flex size-7 items-center justify-center rounded-[28%] border text-muted-foreground">
          <Icon className="size-3.5" aria-hidden="true" />
        </span>
        <div className="min-w-0">
          <div className="flex items-baseline gap-2 text-[0.84375rem] leading-tight">
            <span className="font-semibold text-foreground">{systemTopic ? t(`${systemTopic}Topic.title`) : message.body}</span>
            <time dateTime={message.created_at} className="text-xs text-subtle">
              {formatTime(message.created_at)}
            </time>
          </div>
          {systemTopic ? <p className="mt-1 text-[0.8125rem] text-muted-foreground">{t(`${systemTopic}Topic.hint`)}</p> : null}
          <TopicFooter summary={thread} roomId={message.room_id} selected={selected} onOpen={() => onOpenThread(thread.id)} />
        </div>
      </li>
    )
  }
  if (message.sender_kind === 'system' && offerProjectId) {
    return (
      <li data-message-id={message.id} className="mx-auto grid max-w-215 grid-cols-[1.75rem_minmax(0,1fr)] gap-x-3 px-6 py-[0.6875rem]">
        <span className="flex size-7 items-center justify-center rounded-[28%] border text-muted-foreground">
          <BookHeartIcon className="size-3.5" aria-hidden="true" />
        </span>
        <MaintainerOfferNote projectId={offerProjectId} roomId={message.room_id} />
      </li>
    )
  }
  if (message.sender_kind === 'system') {
    return (
      <li data-message-id={message.id} className="mx-auto max-w-215 px-6 py-2 [contain-intrinsic-size:auto_2rem] [content-visibility:auto]">
        <NoteLine note={systemNote(t, message.body)} time={message.created_at} />
      </li>
    )
  }
  return (
    <li
      data-message-id={message.id}
      className={cn(
        'group/row mx-auto max-w-215 rounded-xl px-6 transition-colors duration-1000 [contain-intrinsic-size:auto_4rem] [content-visibility:auto]',
        continued ? 'pt-0.5 pb-[0.6875rem]' : 'py-[0.6875rem]',
        lit && 'bg-selection duration-200',
      )}
    >
      <div
        className={cn(
          'grid grid-cols-[1.75rem_minmax(0,1fr)] gap-x-3 rounded-xl transition-colors',
          // The message whose topic is open, lifted a little off the chat.
          selected && '-mx-3 -my-2 bg-foreground/[0.03] px-3 py-2 ring-1 ring-foreground/[0.06] ring-inset',
        )}
      >
        {continued ? (
          <span aria-hidden="true" />
        ) : sender.look ? (
          <AgentAvatar look={sender.look} name={sender.name} size="message" />
        ) : (
          <UserAvatar name={sender.name} size="message" />
        )}
        <div className="min-w-0">
          {continued ? null : (
            <div className="mb-1 flex h-7 items-center gap-2 text-[0.84375rem] leading-tight">
              <span className="truncate font-semibold text-foreground">{sender.name}</span>
              {leader ? (
                <span className="flex-none rounded-[0.3125rem] border px-1.5 text-[0.6875rem] leading-4 font-medium text-muted-foreground">
                  {t('member.leader')}
                </span>
              ) : null}
              <time dateTime={message.created_at} className="flex-none text-xs text-subtle">
                {formatTime(message.created_at)}
              </time>
            </div>
          )}
          <Body message={message} names={names} liveText={live?.text} waiting={approval} />
          <MessageAttachments attachments={message.attachments} roomId={message.room_id} threadId={message.thread_id} />
          {thread && onOpenThread ? (
            <TopicFooter
              summary={thread}
              roomId={message.room_id}
              liveTool={live?.tool}
              compacting={live?.compacting}
              waiting={approval ? { what: approvalCommand(approval), kind: askKind(approval.kind) } : undefined}
              worker={worker}
              selected={selected}
              onOpen={() => onOpenThread(thread.id)}
            />
          ) : null}
        </div>
      </div>
    </li>
  )
}, areEqual)

// An empty body is a topic root the agent has not filled in yet: while its
// turn runs, the text streaming in stands in for it, or what the turn waits
// on a person for. A person's empty body means files alone, drawn below.
// An agent's long answer shows its start in the chat, the rest on asking.
function Body({ message, names, liveText, waiting }: { message: RoomMessage; names: Map<string, string>; liveText?: string; waiting?: Approval }) {
  if (message.sender_kind === 'user' && message.body === '') return null
  if (message.body !== '') {
    if (message.sender_kind === 'agent') {
      return (
        <Clamped>
          <AgentBody message={message} names={names} target="room" />
        </Clamped>
      )
    }
    return <MessageBody body={message.body} mentions={message.mentions} names={names} className={`${bodyClass} whitespace-pre-wrap`} />
  }
  const running = message.thread?.last_turn?.status === 'running'
  if (running && liveText) {
    return <AgentBody message={{ ...message, body: liveText }} names={names} target="room" streaming />
  }
  if (running) {
    return (
      <div className={bodyClass}>
        <Shimmer as="span" className="text-[0.90625rem]">
          {waiting ? t(waitingKeys[askKind(waiting.kind)].turn) : t('message.typing')}
        </Shimmer>
      </div>
    )
  }
  return <div className={`${bodyClass} text-subtle`}>{t('message.empty')}</div>
}

function areEqual(prev: MessageRowProps, next: MessageRowProps) {
  return (
    prev.message === next.message &&
    prev.sender.name === next.sender.name &&
    prev.sender.kind === next.sender.kind &&
    prev.sender.runtime === next.sender.runtime &&
    prev.sender.look?.avatar === next.sender.look?.avatar &&
    prev.names === next.names &&
    prev.approval === next.approval &&
    prev.systemTopic === next.systemTopic &&
    prev.offerProjectId === next.offerProjectId &&
    prev.onOpenThread === next.onOpenThread &&
    prev.continued === next.continued &&
    prev.selected === next.selected &&
    prev.leader === next.leader &&
    prev.worker === next.worker &&
    prev.lit === next.lit
  )
}
