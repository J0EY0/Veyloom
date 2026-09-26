import { useCallback } from 'react'
import { useProject } from '@/api/projects'
import { useRoom } from '@/api/rooms'
import type { Mention, Message } from '@/api/types'
import { AgentMarkdown } from '@/components/shared/agent-markdown'
import { insertIntoComposer, type ComposerTarget } from '@/lib/composer'

export interface AgentBodyProps {
  message: Message
  names: Map<string, string>
  // Where a take-over lands: 'room' or the topic's thread id.
  target: ComposerTarget
  // True while the text is still arriving.
  streaming?: boolean
  className?: string
}

// An agent's text is markdown. Naming another agent in it wakes that one
// (docs/design.md 5.22), unless the project's agents wake no one: there a
// mention is a hand-off a person may pick up, a button that prefills the
// composer with that @ (docs/webui.md §7 step 8).
export function AgentBody({ message, names, target, streaming, className }: AgentBodyProps) {
  const room = useRoom(message.room_id)
  const project = useProject(room.data?.project_id ?? '')
  const handsOff = project?.relay_limit === -1
  const takeOver = useCallback((_mention: Mention, name: string) => insertIntoComposer(target, `@${name} `), [target])
  return (
    <AgentMarkdown
      text={message.body}
      mentions={message.mentions}
      names={names}
      streaming={streaming}
      onTakeOver={handsOff ? takeOver : undefined}
      className={className}
    />
  )
}
