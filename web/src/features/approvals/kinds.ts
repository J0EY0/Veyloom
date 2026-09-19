import type { MessageKey } from '@/i18n/zh-CN'

// AskKind is what a request asks of a person: permission to use a tool,
// answers to questions, a form filled in or a link opened (docs/design.md
// 4.6).
export type AskKind = 'tool_use' | 'question' | 'form' | 'link'

export function askKind(kind: string | undefined): AskKind {
  return kind === 'question' || kind === 'form' || kind === 'link' ? kind : 'tool_use'
}

// How a turn reads while each kind of request waits on a person, in each
// place that says so: the turn itself, a topic's footer, a member's state
// and the island over the feed.
export const waitingKeys: Record<AskKind, { turn: MessageKey; topic: MessageKey; member: MessageKey; island: MessageKey }> = {
  tool_use: { turn: 'turn.waitingApproval', topic: 'topic.waiting', member: 'member.waiting', island: 'island.waiting' },
  question: { turn: 'turn.waitingAnswer', topic: 'topic.asking', member: 'member.asking', island: 'island.asking' },
  form: { turn: 'turn.waitingForm', topic: 'topic.form', member: 'member.form', island: 'island.form' },
  link: { turn: 'turn.waitingLink', topic: 'topic.link', member: 'member.link', island: 'island.link' },
}
