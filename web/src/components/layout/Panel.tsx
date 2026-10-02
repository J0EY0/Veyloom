import type { ReactNode, Ref } from 'react'
import { cn } from '@/lib/utils'

export interface PanelProps {
  children: ReactNode
  className?: string
  ref?: Ref<HTMLElement>
}

// The page surface: every route renders into one, filling the card the
// shell draws next to the sidebar (docs/webui.md §0). It is the container
// a page's layout asks how wide it is (@container/panel): a list and what
// it opens sit side by side only from the theme's split width
// (@split/panel:), however wide the window, since the sidebar beside a
// tablet's or a narrow window's page leaves it a phone's width.
export function Panel({ children, className, ref }: PanelProps) {
  return (
    <section ref={ref} className={cn('@container/panel relative flex h-full min-h-0 flex-col overflow-hidden', className)}>
      {children}
    </section>
  )
}
