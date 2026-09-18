import { Fragment, useMemo } from 'react'
import type { Mention } from '@/api/types'
import { MentionPill } from '@/components/shared/mention-pill'

export interface MessageBodyProps {
  body: string
  mentions: Mention[] | null
  names: Map<string, string>
  className?: string
}

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

// Plain text with the structured mentions drawn as pills. Only names the
// message actually mentions are matched; the body is never parsed for @.
export function MessageBody({ body, mentions, names, className }: MessageBodyProps) {
  const parts = useMemo(() => split(body, mentions, names), [body, mentions, names])
  return (
    <div className={className}>
      {parts.map((part, index) => (part.pill ? <MentionPill key={index} name={part.text} id={part.id} /> : <Fragment key={index}>{part.text}</Fragment>))}
    </div>
  )
}

interface Part {
  text: string
  pill: boolean
  // Who a pill names.
  id?: string
}

export function split(body: string, mentions: Mention[] | null, names: Map<string, string>): Part[] {
  const idOf = new Map<string, string>()
  for (const m of mentions ?? []) {
    const name = names.get(m.id)
    if (name && !idOf.has(name)) idOf.set(name, m.id)
  }
  const mentioned = [...idOf.keys()]
  if (mentioned.length === 0) return [{ text: body, pill: false }]
  // Longest first so "Codex Implementer B" is not eaten by "Codex Implementer".
  const pattern = new RegExp(
    `@(${[...new Set(mentioned)]
      .sort((a, b) => b.length - a.length)
      .map(escape)
      .join('|')})`,
    'g',
  )
  const parts: Part[] = []
  let last = 0
  for (const match of body.matchAll(pattern)) {
    if (match.index > last) parts.push({ text: body.slice(last, match.index), pill: false })
    parts.push({ text: match[1], pill: true, id: idOf.get(match[1]) })
    last = match.index + match[0].length
  }
  if (last < body.length) parts.push({ text: body.slice(last), pill: false })
  return parts
}
