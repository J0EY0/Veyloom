import type { Approval, Message } from '@/api/types'
import { MessageAttachments } from '@/features/attachments/MessageAttachments'
import { RequestCard } from '@/features/approvals/RequestCard'
import { AgentBody } from '@/features/rooms/AgentBody'
import { MessageBody } from '@/features/rooms/MessageBody'
import type { Sender } from '@/features/rooms/useSenderNames'
import { ThreadNote } from './ThreadNote'
import { ThreadRow } from './ThreadRow'

export interface ThreadMessageProps {
  message: Message
  sender: Sender
  // The approval this system note announces, which draws it as a card.
  approval?: Approval
  names?: Map<string, string>
  // Where a take-over from this message lands; the topic's thread id.
  target?: string
  // Inside an agent's turn, whose row already says who is speaking.
  inTurn?: boolean
}

// What one message looks like inside a topic, as the chat draws it: face,
// name and time over what was said; inside an agent's turn just its words.
// The system speaks in a quiet line, and a permission request is a card.
export function ThreadMessage({ message, sender, approval, names, target = 'room', inTurn }: ThreadMessageProps) {
  const known = names ?? new Map<string, string>()
  if (approval) {
    return <RequestCard approval={approval} names={known} />
  }
  if (message.sender_kind === 'system') {
    return <ThreadNote message={message} />
  }
  const words =
    message.sender_kind === 'agent' ? (
      <AgentBody message={message} names={known} target={target} className="text-[0.875rem]" />
    ) : message.body !== '' ? (
      <MessageBody
        body={message.body}
        mentions={message.mentions}
        names={known}
        className="text-[0.875rem] leading-[1.6] break-words whitespace-pre-wrap text-body"
      />
    ) : null
  const content = (
    <>
      {words}
      <MessageAttachments attachments={message.attachments} roomId={message.room_id} threadId={message.thread_id} className="mt-1.5" />
    </>
  )
  if (inTurn) {
    return <div className="mt-0.5">{content}</div>
  }
  return (
    <ThreadRow sender={sender} time={message.created_at}>
      {content}
    </ThreadRow>
  )
}
