import type { ReactNode } from 'react'
import { Link, type To } from 'react-router'
import { cn } from '@/lib/utils'

export interface HeaderTab {
  to: To
  active: boolean
  label: ReactNode
}

// The views of a page, side by side in its top bar: the chat and its Wiki,
// the library's skills and its patterns. Each is a link, so the view is in
// the address. On a phone the row scrolls sideways rather than push the
// bar's buttons off, and the open view scrolls into sight.
export function HeaderTabs({ label, tabs }: { label: string; tabs: HeaderTab[] }) {
  return (
    <nav aria-label={label} className="ml-1 flex min-w-0 [scrollbar-width:none] items-center gap-0.5 overflow-x-auto rounded-lg bg-muted p-0.5">
      {tabs.map((tab, index) => (
        <Link
          key={index}
          to={tab.to}
          ref={tab.active ? (node) => node?.scrollIntoView({ block: 'nearest', inline: 'nearest' }) : undefined}
          aria-current={tab.active ? 'page' : undefined}
          className={cn(
            'flex h-6.5 flex-none items-center gap-1.5 rounded-md px-2.5 text-[0.8125rem] font-medium whitespace-nowrap text-muted-foreground outline-hidden transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring',
            tab.active && 'bg-background text-foreground shadow-xs',
          )}
        >
          {tab.label}
        </Link>
      ))}
    </nav>
  )
}
