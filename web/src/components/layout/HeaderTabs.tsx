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
// the address.
export function HeaderTabs({ label, tabs }: { label: string; tabs: HeaderTab[] }) {
  return (
    <nav aria-label={label} className="ml-1 flex flex-none items-center gap-0.5 rounded-lg bg-muted p-0.5">
      {tabs.map((tab, index) => (
        <Link
          key={index}
          to={tab.to}
          aria-current={tab.active ? 'page' : undefined}
          className={cn(
            'flex h-6.5 items-center gap-1.5 rounded-md px-2.5 text-[0.8125rem] font-medium text-muted-foreground outline-hidden transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring',
            tab.active && 'bg-background text-foreground shadow-xs',
          )}
        >
          {tab.label}
        </Link>
      ))}
    </nav>
  )
}
