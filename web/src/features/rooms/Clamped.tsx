import { useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { ChevronDownIcon } from 'lucide-react'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { lineCut } from './lineCut'

// How tall a message stands in the chat before the rest waits to be asked
// for: three lines of its words.
const CLAMP_REM = 4.5

// Clamped shows the start of a long message in the chat and the rest when
// asked (docs/webui.md §4.1): the chat reads as a list of what was said,
// and the topic has it all.
export function Clamped({ children }: { children: ReactNode }) {
  const t = useT()
  const content = useRef<HTMLDivElement>(null)
  // How far down it is cut, in rem; none while it all fits.
  const [cut, setCut] = useState<number>()
  const [open, setOpen] = useState(false)
  useLayoutEffect(() => {
    const element = content.current
    if (!element) return
    const measure = () => {
      const rem = parseFloat(getComputedStyle(document.documentElement).fontSize)
      const limit = CLAMP_REM * rem
      // A line more than fits is not worth a button.
      setCut(element.offsetHeight > limit * 1.4 ? lineCut(element, limit) / rem : undefined)
    }
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [])
  const long = cut !== undefined
  const shut = long && !open
  return (
    <>
      {/* Cut after a whole line, and that line fades out, rather than
          stopping in the middle of one. */}
      <div
        className={cn(shut && 'overflow-hidden [mask-image:linear-gradient(to_bottom,black_calc(100%_-_1.5rem),transparent)]')}
        style={shut ? { maxHeight: `${cut}rem` } : undefined}
      >
        <div ref={content}>{children}</div>
      </div>
      {long ? (
        <button
          type="button"
          aria-expanded={open}
          onClick={() => setOpen((was) => !was)}
          className="mt-1 inline-flex items-center gap-1 rounded-sm text-[0.78125rem] text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50"
        >
          {open ? t('message.collapse') : t('message.expand')}
          <ChevronDownIcon aria-hidden="true" className={cn('size-3.5 transition-transform duration-200', open && 'rotate-180')} />
        </button>
      ) : null}
    </>
  )
}
