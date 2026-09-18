import { useCallback, useMemo, useState, type KeyboardEvent, type RefObject } from 'react'
import type { MentionTarget } from './useMentionTargets'

export interface MentionInput {
  open: boolean
  items: MentionTarget[]
  active: number
  // Call from the textarea's input handler to track the @ token at the caret.
  onInput: () => void
  // Call first from the textarea's keydown handler; true means it was consumed.
  onKeyDown: (event: KeyboardEvent<HTMLTextAreaElement>) => boolean
  select: (item: MentionTarget) => void
  close: () => void
}

interface Token {
  start: number
  query: string
}

const maxItems = 6

// useMentionInput turns "@" typed in a textarea into a picker over
// targets: it watches the token at the caret, filters by prefix, moves
// the highlight with the arrows and writes "@Name " back on selection.
export function useMentionInput(textareaRef: RefObject<HTMLTextAreaElement | null>, targets: MentionTarget[]): MentionInput {
  const [token, setToken] = useState<Token | null>(null)
  const [active, setActive] = useState(0)

  const items = useMemo(() => (token ? filter(targets, token.query) : []), [targets, token])
  const open = token !== null && items.length > 0

  const onInput = useCallback(() => {
    const el = textareaRef.current
    if (!el) return
    const next = tokenAt(el.value, el.selectionStart)
    setToken(next)
    setActive(0)
  }, [textareaRef])

  const close = useCallback(() => setToken(null), [])

  const select = useCallback(
    (item: MentionTarget) => {
      const el = textareaRef.current
      if (!el || !token) return
      const before = el.value.slice(0, token.start)
      const after = el.value.slice(token.start + 1 + token.query.length)
      const inserted = `@${item.name} `
      el.value = before + inserted + after
      const caret = before.length + inserted.length
      el.setSelectionRange(caret, caret)
      el.focus()
      setToken(null)
    },
    [textareaRef, token],
  )

  const onKeyDown = useCallback(
    (event: KeyboardEvent<HTMLTextAreaElement>) => {
      if (!open) return false
      switch (event.key) {
        case 'ArrowDown':
          setActive((i) => (i + 1) % items.length)
          break
        case 'ArrowUp':
          setActive((i) => (i - 1 + items.length) % items.length)
          break
        case 'Enter':
        case 'Tab':
          select(items[active])
          break
        case 'Escape':
          close()
          break
        default:
          return false
      }
      event.preventDefault()
      return true
    },
    [open, items, active, select, close],
  )

  return { open, items, active, onInput, onKeyDown, select, close }
}

// tokenAt finds an "@word" the caret is inside or right after; the @ must
// start the text or follow whitespace so emails and the like are left alone.
export function tokenAt(value: string, caret: number): Token | null {
  const before = value.slice(0, caret)
  const at = before.lastIndexOf('@')
  if (at === -1) return null
  if (at > 0 && !/\s/.test(before[at - 1])) return null
  const query = before.slice(at + 1)
  if (/\s/.test(query)) return null
  return { start: at, query }
}

function filter(targets: MentionTarget[], query: string): MentionTarget[] {
  const q = query.toLowerCase()
  return targets.filter((target) => target.name.toLowerCase().startsWith(q)).slice(0, maxItems)
}
