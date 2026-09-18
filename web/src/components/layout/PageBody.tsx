import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

export interface PageBodyProps {
  // One line under the header saying what the page is for.
  description?: ReactNode
  // 'wide' is for grids of cards, 'narrow' for a single column of rows.
  width?: 'wide' | 'narrow'
  // Stretch the column to the body's height, so an Empty after the header
  // takes the rest of it and sits in the middle instead of under the header.
  fill?: boolean
  children: ReactNode
  className?: string
}

// The scrolling body of a management page: one measure, one gutter, and
// the sentence that tells you what you are looking at (docs/webui.md §0).
export function PageBody({ description, width = 'wide', fill = false, children, className }: PageBodyProps) {
  return (
    <div className={cn('min-h-0 flex-1 overflow-y-auto px-6 pb-8', fill && 'flex flex-col', className)}>
      <div className={cn('mx-auto flex flex-col gap-4', width === 'wide' ? 'max-w-280' : 'max-w-205', fill && 'w-full flex-1')}>
        {description ? <p className="max-w-[80ch] text-[0.8125rem] leading-relaxed text-subtle">{description}</p> : null}
        {children}
      </div>
    </div>
  )
}
