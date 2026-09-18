import type { ReactNode } from 'react'
import { Link } from 'react-router'
import { Breadcrumb, BreadcrumbItem, BreadcrumbLink, BreadcrumbList, BreadcrumbPage, BreadcrumbSeparator } from '@/components/ui/breadcrumb'
import { SidebarTrigger } from '@/components/ui/sidebar'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { Kbd } from '@/components/ui/kbd'
import { useT } from '@/lib/i18n'

export interface PanelHeaderProps {
  title: ReactNode
  // A muted glyph before the title, like the # of a room.
  prefix?: string
  // The page this one sits under, shown as a breadcrumb before the title.
  parent?: { to: string; label: ReactNode }
  // Sits right after the title.
  actions?: ReactNode
  // Sits at the far right.
  trailing?: ReactNode
}

// The top row of a page: a sidebar toggle, the title, and whatever the
// page puts on either side of the gap. No divider under it; spacing does
// the separating.
export function PanelHeader({ title, prefix, parent, actions, trailing }: PanelHeaderProps) {
  const t = useT()
  return (
    <header className="flex h-12 flex-none items-center gap-1 pr-3 pl-3">
      <Tooltip>
        <TooltipTrigger asChild>
          <SidebarTrigger aria-label={t('common.toggleSidebar')} className="mr-1 text-subtle" />
        </TooltipTrigger>
        <TooltipContent side="bottom">
          {t('common.toggleSidebar')} <Kbd>⌘B</Kbd>
        </TooltipContent>
      </Tooltip>
      {parent ? (
        <Breadcrumb className="mr-1 min-w-0">
          <BreadcrumbList className="flex-nowrap gap-1.5 sm:gap-1.5">
            <BreadcrumbItem className="flex-none">
              <BreadcrumbLink asChild>
                <Link to={parent.to}>{parent.label}</Link>
              </BreadcrumbLink>
            </BreadcrumbItem>
            <BreadcrumbSeparator />
            <BreadcrumbItem className="min-w-0">
              <h1 className="min-w-0 truncate text-sm font-semibold tracking-[-0.01em]">
                <BreadcrumbPage className="font-semibold">{title}</BreadcrumbPage>
              </h1>
            </BreadcrumbItem>
          </BreadcrumbList>
        </Breadcrumb>
      ) : (
        <h1 className="mr-1 flex min-w-0 items-center truncate text-sm font-semibold tracking-[-0.01em]">
          {prefix ? <span className="mr-[0.1875rem] font-normal text-subtle">{prefix}</span> : null}
          {title}
        </h1>
      )}
      {actions}
      <span className="grow" />
      {trailing}
    </header>
  )
}
