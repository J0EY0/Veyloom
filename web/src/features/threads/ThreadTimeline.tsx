import type { Message, Turn } from '@/api/types'
import type { Sender } from '@/features/rooms/useSenderNames'
import { ThreadMessage } from './ThreadMessage'
import { TurnPart } from './TurnPart'

export interface ThreadTimelineProps {
  root: Message
  replies: Message[]
  turns: Turn[]
  sender: (message: Message) => Sender
  names?: Map<string, string>
  onOpenTurn?: (turnId: string) => void
  target?: string
  // The root names the topic in the header; it shows here only when the
  // title is opened.
  withRoot?: boolean
}

// A stretch of the topic: a message on its own, or what one turn of an
// agent said in a row, drawn under a single face.
export type Part = { kind: 'message'; message: Message } | { kind: 'turn'; turn: Turn; messages: Message[]; first: boolean; last: boolean }

// The topic read as a chat (docs/webui.md §4.2): what people and agents said,
// in order, each under their face and name. There is no heading per turn;
// what a turn did folds into one line of its answer.
export function ThreadTimeline({ root, replies, turns, sender, names, onOpenTurn, target, withRoot }: ThreadTimelineProps) {
  const parts = buildParts(root, replies, turns, withRoot)
  return (
    <div className="flex flex-col">
      {parts.map((part, index) =>
        part.kind === 'turn' ? (
          <TurnPart
            key={`${part.turn.id}-${index}`}
            turn={part.turn}
            messages={part.messages}
            who={sender(part.messages.find((message) => message.sender_kind === 'agent') ?? memberMessage(part.turn))}
            first={part.first}
            last={part.last}
            names={names}
            onOpenTurn={onOpenTurn}
            target={target}
          />
        ) : (
          <ThreadMessage key={part.message.id} message={part.message} sender={sender(part.message)} names={names} target={target} />
        ),
      )}
    </div>
  )
}

export function buildParts(root: Message, replies: Message[], turns: Turn[], withRoot = false): Part[] {
  const turnOf = new Map(turns.map((turn) => [turn.id, turn]))
  const parts: Part[] = []
  // The root, the message the topic hangs from in the chat, is the panel's
  // title unless it is opened.
  const items = withRoot ? [root, ...replies] : replies
  for (const message of items) {
    // An unfilled root says nothing yet; its turn shows the words arriving.
    if (message.id === root.id && message.body === '') continue
    const turn = message.sender_kind === 'user' ? undefined : turnOf.get(message.turn_id ?? '')
    const previous = parts[parts.length - 1]
    if (!turn) {
      parts.push({ kind: 'message', message })
    } else if (previous?.kind === 'turn' && previous.turn.id === turn.id) {
      previous.messages.push(message)
    } else {
      parts.push({ kind: 'turn', turn, messages: [message], first: false, last: false })
    }
  }
  // A turn at work that has said nothing yet still shows who is on it.
  for (const turn of turns) {
    if (turn.status === 'running' && !parts.some((part) => part.kind === 'turn' && part.turn.id === turn.id)) {
      parts.push({ kind: 'turn', turn, messages: [], first: false, last: false })
    }
  }
  const firsts = new Set<string>()
  for (const part of parts) {
    if (part.kind === 'turn' && !firsts.has(part.turn.id)) {
      firsts.add(part.turn.id)
      part.first = true
    }
  }
  const lasts = new Set<string>()
  for (const part of [...parts].reverse()) {
    if (part.kind === 'turn' && !lasts.has(part.turn.id)) {
      lasts.add(part.turn.id)
      part.last = true
    }
  }
  return parts
}

// Stands for a turn's member when the turn has said nothing yet, so it is
// named and drawn like any agent message.
function memberMessage(turn: Turn): Message {
  return { id: '', seq: 0, room_id: turn.room_id, sender_kind: 'agent', member_id: turn.member_id, body: '', mentions: null, created_at: turn.started_at }
}
