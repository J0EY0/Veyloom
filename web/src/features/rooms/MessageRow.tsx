import { memo } from 'react'
import type { Approval, RoomMessage } from '@/api/types'
import { Shimmer } from '@/components/ai-elements/shimmer'
import { AgentAvatar } from '@/components/shared/agent-avatar'
import { AttachmentList } from '@/components/shared/attachment-list'
import { UserAvatar } from '@/components/shared/user-avatar'
import { Badge } from '@/components/ui/badge'
import { approvalCommand } from '@/features/approvals/describe'
import { askKind, waitingKeys } from '@/features/approvals/kinds'
import { runtimeName } from '@/lib/runtimes'
import { formatTime } from '@/lib/format'
import { useLiveTurn } from '@/lib/liveTurns'
import { AgentBody } from './AgentBody'
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
  onOpenThread?: (threadId: string) => void
}

const bodyClass = 'text-[0.90625rem] leading-[1.6] break-words text-body'

// One top-level message: avatar, name, time, body, and under a topic root
// the topic's footer. System notes are a single muted line. Rows are
// memoised so a new message at the bottom does not re-render the fifty
// above it.
export const MessageRow = memo(function MessageRow({ message, sender, names, approval, onOpenThread }: MessageRowProps) {
  const thread = message.thread
  const runningTurnId = thread?.last_turn?.status === 'running' ? thread.last_turn.id : undefined
  const live = useLiveTurn(runningTurnId)
  if (message.sender_kind === 'system') {
    return <li className="mx-auto max-w-215 px-6 py-1.5 text-xs text-subtle [contain-intrinsic-size:auto_28px] [content-visibility:auto]">{message.body}</li>
  }
  return (
    <li className="group/row mx-auto grid max-w-215 grid-cols-[24px_minmax(0,1fr)] gap-x-3 px-6 py-[0.6875rem] [contain-intrinsic-size:auto_64px] [content-visibility:auto]">
      {sender.look ? <AgentAvatar look={sender.look} /> : <UserAvatar name={sender.name} />}
      <div className="min-w-0">
        <div className="mb-1 flex items-center gap-2 text-[0.8125rem] leading-tight">
          <span className="truncate font-medium text-foreground">{sender.name}</span>
          {sender.runtime ? (
            <Badge variant="outline" className="h-4.5 rounded-[5px] px-1.5 text-[0.65625rem] font-normal text-subtle" translate="no">
              {runtimeName(sender.runtime)}
            </Badge>
          ) : null}
          <time dateTime={message.created_at} className="flex-none text-xs text-subtle">
            {formatTime(message.created_at)}
          </time>
        </div>
        <Body message={message} names={names} liveText={live?.text} waiting={approval} />
        <AttachmentList attachments={message.attachments} />
        {thread && onOpenThread ? (
          <TopicFooter
            summary={thread}
            liveTool={live?.tool}
            compacting={live?.compacting}
            waiting={approval ? { what: approvalCommand(approval), kind: askKind(approval.kind) } : undefined}
            onOpen={() => onOpenThread(thread.id)}
          />
        ) : null}
      </div>
    </li>
  )
}, areEqual)

// An empty body is a topic root the agent has not filled in yet: while its
// turn runs, the text streaming in stands in for it, or what the turn waits
// on a person for. A person's empty body means files alone, drawn below.
function Body({ message, names, liveText, waiting }: { message: RoomMessage; names: Map<string, string>; liveText?: string; waiting?: Approval }) {
  if (message.sender_kind === 'user' && message.body === '') return null
  if (message.body !== '') {
    if (message.sender_kind === 'agent') {
      return <AgentBody message={message} names={names} target="room" />
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
    prev.onOpenThread === next.onOpenThread
  )
}
