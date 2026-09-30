import type { Mention } from '@/api/types'
import { ChatMarkdown } from '@/components/shared/chat-markdown'

export interface MessageBodyProps {
  body: string
  mentions: Mention[] | null
  names: Map<string, string>
  className?: string
}

// A person's words, drawn as an agent's are: markdown, so that code, lists
// and emphasis read as meant, with the lines and characters they typed
// kept, and the structured mentions as pills. Only names the message
// mentions become pills; the body is never parsed for @.
export function MessageBody({ body, mentions, names, className }: MessageBodyProps) {
  return <ChatMarkdown text={body} mentions={mentions} names={names} typed className={className} />
}
