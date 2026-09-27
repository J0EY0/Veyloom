import { useMemo } from 'react'
import { MessageResponse } from '@/components/ai-elements/message'
import type { Mention } from '@/api/types'
import { cn } from '@/lib/utils'
import { MentionPill, TakeOverButton } from './mention-pill'

export interface AgentMarkdownProps {
  text: string
  mentions: Mention[] | null
  names: Map<string, string>
  // Called for an agent mention the reader clicks: the take-over button.
  onTakeOver?: (mention: Mention, name: string) => void
  // True while the text is still arriving: unfinished markdown is closed
  // for display and a caret blinks at the end.
  streaming?: boolean
  className?: string
}

// Mentions travel through the markdown as a <mention> tag Streamdown is
// told to keep and to treat as plain text, which the renderer turns into
// a pill (docs/webui.md §4.1).
const allowedTags = { mention: ['kind', 'id'] }
const literalTags = ['mention']

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

export function withMentionTags(text: string, mentions: Mention[] | null, names: Map<string, string>): string {
  const known = (mentions ?? []).flatMap((m) => {
    const name = names.get(m.id)
    return name ? [{ ...m, name }] : []
  })
  if (known.length === 0) return text
  const byName = new Map(known.map((m) => [m.name, m]))
  // Every name competes for an @, longest first, so "Codex Implementer B"
  // is not eaten by "Codex Implementer" even when only the latter is
  // mentioned; a name the message does not mention stays text.
  const pattern = new RegExp(
    `@(${[...new Set([...byName.keys(), ...names.values()])]
      .filter((name) => name !== '')
      .sort((a, b) => b.length - a.length)
      .map(escape)
      .join('|')})`,
    'g',
  )
  return outsideCode(text, (prose) =>
    prose.replace(pattern, (match, name: string) => {
      const m = byName.get(name)
      return m ? `<mention kind="${m.kind}" id="${m.id}">@${name}</mention>` : match
    }),
  )
}

// A fenced code block's opening or closing line, and a code span on a line.
const fenceLine = /^ {0,3}(`{3,}|~{3,})/
const inlineCode = /(`+[^`\n]*`+)/

// outsideCode changes the prose of markdown alone: code, a fenced block or
// a span, is shown as written, so a name in it is no mention (the hub reads
// it the same way). A block left open, as while a reply streams in, runs
// to the end.
export function outsideCode(markdown: string, change: (prose: string) => string): string {
  let fence = ''
  return markdown
    .split('\n')
    .map((line) => {
      const mark = fenceLine.exec(line)?.[1]
      if (fence !== '') {
        if (mark && mark[0] === fence[0] && mark.length >= fence.length) fence = ''
        return line
      }
      if (mark) {
        fence = mark
        return line
      }
      return line
        .split(inlineCode)
        .map((piece, index) => (index % 2 === 1 ? piece : change(piece)))
        .join('')
    })
    .join('\n')
}

// Agent text is markdown rendered by Streamdown (docs/webui.md §2), which
// also handles the incomplete markdown of a reply still streaming in.
export function AgentMarkdown({ text, mentions, names, onTakeOver, streaming, className }: AgentMarkdownProps) {
  const source = useMemo(() => withMentionTags(text, mentions, names), [text, mentions, names])
  const components = useMemo(
    () => ({
      mention: ({ kind, id, children }: { kind?: string; id?: string; children?: React.ReactNode }) => {
        const name = (id && names.get(id)) || String(children ?? '').replace(/^@/, '')
        if (kind === 'agent' && id && onTakeOver) {
          return <TakeOverButton name={name} id={id} onClick={() => onTakeOver({ kind: 'agent', id }, name)} />
        }
        return <MentionPill name={name} id={kind === 'agent' ? id : undefined} />
      },
    }),
    [names, onTakeOver],
  )
  return (
    // MessageResponse re-renders only when its text changes, and a stream
    // ends on the very text the message keeps: a new key drops the caret.
    <MessageResponse
      key={streaming ? 'streaming' : 'static'}
      mode={streaming ? 'streaming' : 'static'}
      parseIncompleteMarkdown={streaming}
      isAnimating={streaming}
      caret={streaming ? 'block' : undefined}
      allowedTags={allowedTags}
      literalTagContent={literalTags}
      components={components}
      controls={false}
      lineNumbers={false}
      className={cn('prose-agent', className)}
    >
      {source}
    </MessageResponse>
  )
}
