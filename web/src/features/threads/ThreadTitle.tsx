import { useLayoutEffect, useState } from 'react'
import { ChevronDownIcon } from 'lucide-react'
import type { Message } from '@/api/types'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'

export interface ThreadTitleProps {
  // What the topic is called in its chat, the 12 of #12: what people say
  // and what agents read topics by. Absent until the topic is known.
  number?: number
  // The message the topic hangs from in the chat, its root.
  root: Message
  // Names the topic while the root has nothing in it yet: the message that
  // started the turn filling it.
  fallback?: Message
  // Whether the root shows in full at the top of the topic.
  open: boolean
  onToggle: () => void
}

// The topic's name in the panel header: the words of the message it hangs
// from in the chat, on one line, so that message is not repeated below.
// When they do not fit, run over lines or bring files, the title is a
// button that shows the whole message at the top of the topic.
export function ThreadTitle({ number, root, fallback, open, onToggle }: ThreadTitleProps) {
  const t = useT()
  const [element, setElement] = useState<HTMLElement | null>(null)
  const [cut, setCut] = useState(false)
  const filled = root.body !== '' || (root.attachments?.length ?? 0) > 0
  const text = (filled ? titleOf(root) : fallback ? titleOf(fallback) : '') || t('thread.label')
  useLayoutEffect(() => {
    if (!element) return
    const measure = () => setCut(element.scrollWidth > element.clientWidth)
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [element, text])

  const more = filled && (cut || root.body.trim().includes('\n') || (root.attachments?.length ?? 0) > 0)
  const words = (
    <span ref={setElement} className="truncate">
      {text}
    </span>
  )
  return (
    <h2 className="flex min-w-0 items-center gap-1.5 text-sm font-semibold">
      {number ? (
        <span className="flex-none font-normal text-subtle tabular-nums" translate="no">
          #{number}
        </span>
      ) : null}
      {more || open ? (
        <button
          type="button"
          aria-expanded={open}
          onClick={onToggle}
          className="flex min-w-0 items-center gap-1 rounded-md text-left outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
        >
          {words}
          <ChevronDownIcon aria-hidden="true" className={cn('size-3.5 flex-none text-subtle transition-transform duration-200', open && 'rotate-180')} />
        </button>
      ) : (
        words
      )}
    </h2>
  )
}

// A message's words on one line, the marks of an agent's markdown taken
// off; one that brought only files goes by the first file's name.
export function titleOf(message: Message): string {
  const words = message.body
    .replace(/```[^\n]*/g, ' ')
    .replace(/`([^`\n]*)`/g, '$1')
    .replace(/!?\[([^\]\n]*)\]\([^)\n]*\)/g, '$1')
    .replace(/^[ \t]*(?:#{1,6}|>|[-*+]|\d+\.)[ \t]+/gm, '')
    .replace(/(\*\*|__|~~)(\S(?:.*?\S)?)\1/g, '$2')
    .replace(/\s+/g, ' ')
    .trim()
  return words || message.attachments?.[0]?.filename || ''
}
