import type { Message } from '@/api/types'
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

// An agent's text is markdown; a mention of another agent in it is a
// hand-off a person may pick up, so it is a button that prefills the
// composer with that @ (docs/webui.md §7 step 8).
export function AgentBody({ message, names, target, streaming, className }: AgentBodyProps) {
  return (
    <AgentMarkdown
      text={message.body}
      mentions={message.mentions}
      names={names}
      streaming={streaming}
      onTakeOver={(_mention, name) => insertIntoComposer(target, `@${name} `)}
      className={className}
    />
  )
}
