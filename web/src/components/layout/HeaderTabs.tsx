import type { ReactNode } from 'react'
import { CheckIcon, ChevronDownIcon } from 'lucide-react'
import { Link, type To } from 'react-router'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { cn } from '@/lib/utils'

export interface HeaderTab {
  to: To
  active: boolean
  label: ReactNode
  // A count after the name, in the row and in the menu; a folded row's
  // button has room for the name alone.
  badge?: ReactNode
}

export interface HeaderTabsProps {
  label: string
  tabs: HeaderTab[]
  // The bar has no room for the row, a phone's or a tablet's beside the
  // sidebar: the open view, and a menu of them all under it.
  folded?: boolean
}

const tab = 'flex h-6.5 items-center gap-1.5 rounded-md px-2.5 text-[0.8125rem] font-medium whitespace-nowrap'

// The views of a page, side by side in its top bar: the chat and its Wiki,
// the library's skills and its patterns. Each is a link, so the view is in
// the address. The row never gives way to the title or what else is in the
// bar, so it is never cut; a bar too narrow for it gets it folded instead,
// which the page decides by the bar's width.
export function HeaderTabs({ label, tabs, folded = false }: HeaderTabsProps) {
  if (folded) {
    const open = tabs.find((candidate) => candidate.active) ?? tabs[0]
    return (
      <nav aria-label={label} className="ml-1 flex-none">
        <DropdownMenu>
          <DropdownMenuTrigger className="flex rounded-lg bg-muted p-0.5 outline-hidden focus-visible:ring-2 focus-visible:ring-ring">
            <span className={cn(tab, 'bg-background pr-1.5 text-foreground shadow-xs')}>
              {open.label}
              <ChevronDownIcon className="-ml-0.5 size-3.5 text-subtle" />
            </span>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="min-w-36">
            {tabs.map((candidate, index) => (
              <DropdownMenuItem key={index} asChild>
                <Link to={candidate.to} aria-current={candidate.active ? 'page' : undefined}>
                  {candidate.label}
                  {candidate.badge}
                  <CheckIcon className={cn('ml-auto', !candidate.active && 'invisible')} />
                </Link>
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </nav>
    )
  }
  return (
    <nav aria-label={label} className="ml-1 flex flex-none items-center gap-0.5 rounded-lg bg-muted p-0.5">
      {tabs.map((candidate, index) => (
        <Link
          key={index}
          to={candidate.to}
          aria-current={candidate.active ? 'page' : undefined}
          className={cn(
            tab,
            'text-muted-foreground outline-hidden transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring',
            candidate.active && 'bg-background text-foreground shadow-xs',
          )}
        >
          {candidate.label}
          {candidate.badge}
        </Link>
      ))}
    </nav>
  )
}
