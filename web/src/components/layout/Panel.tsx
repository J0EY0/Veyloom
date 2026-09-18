import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

export interface PanelProps {
  children: ReactNode
  className?: string
}

// The page surface: every route renders into one, filling the card the
// shell draws next to the sidebar (docs/webui.md §0).
export function Panel({ children, className }: PanelProps) {
  return <section className={cn('relative flex h-full min-h-0 flex-col overflow-hidden', className)}>{children}</section>
}
