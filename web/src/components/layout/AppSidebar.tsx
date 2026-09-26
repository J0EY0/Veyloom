import { useState } from 'react'
import { BookOpenIcon, BotIcon, ChartColumnIcon, InboxIcon, LibraryBigIcon, SearchIcon, ServerIcon, SettingsIcon, SquarePenIcon } from 'lucide-react'
import { Link, useLocation } from 'react-router'
import { usePendingApprovalsAll } from '@/api/approvals'
import { useInbox } from '@/api/inbox'
import { Button } from '@/components/ui/button'
import { Kbd } from '@/components/ui/kbd'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
} from '@/components/ui/sidebar'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { UserMenu } from '@/features/identity/UserMenu'
import { NewProjectDialog } from '@/features/projects/NewProjectDialog'
import { useCurrentUser } from '@/lib/currentUser'
import { useT } from '@/lib/i18n'
import { openPalette } from '@/lib/palette'
import { cn } from '@/lib/utils'
import { SidebarProjects } from './SidebarProjects'
import { WorkspaceMenu } from './WorkspaceMenu'

// The left column (docs/webui.md §0): the workspace, search and new at the
// top; what needs you; the things to manage (agents, machines, the
// projects' wikis, the skill library, settings) under a heading; the
// projects, each of which is a group chat; then the account.
export function AppSidebar() {
  const t = useT()
  const location = useLocation()
  // The inbox counts every pending approval plus what mentioned you that
  // you have not read (docs/webui.md 4.19).
  const pending = usePendingApprovalsAll()
  const pendingCount = pending.data?.length ?? 0
  const user = useCurrentUser()
  const inbox = useInbox(user?.id ?? '')
  const unread = inbox.data?.pages[0]?.unread ?? 0
  const [creating, setCreating] = useState(false)

  return (
    <Sidebar variant="inset" collapsible="offcanvas" className="border-none">
      <SidebarHeader className="flex-row items-center gap-0.5 px-2 pt-2.5 pb-1">
        <WorkspaceMenu />
        <span className="grow" />
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon-sm" aria-label={t('common.search')} onClick={openPalette} className="text-subtle hover:text-foreground">
              <SearchIcon />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">
            {t('common.search')} <Kbd>⌘K</Kbd>
          </TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="outline"
              size="icon-sm"
              aria-label={t('nav.newProject')}
              onClick={() => setCreating(true)}
              className="text-muted-foreground hover:text-foreground"
            >
              <SquarePenIcon />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">{t('nav.newProject')}</TooltipContent>
        </Tooltip>
      </SidebarHeader>

      <SidebarContent className="gap-0 overflow-x-hidden">
        <SidebarGroup className="py-1">
          <SidebarGroupContent>
            <SidebarMenu aria-label={t('nav.main')}>
              <NavItem
                to="/inbox"
                label={t('nav.inbox')}
                icon={<InboxIcon />}
                active={location.pathname === '/inbox'}
                count={pendingCount + unread}
                urgent={pendingCount > 0}
              />
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>

        <SidebarGroup className="py-1">
          <SidebarGroupLabel className="h-7 text-[0.78125rem] text-subtle">{t('nav.manage')}</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu aria-label={t('nav.manage')}>
              <NavItem to="/agents" label={t('nav.agents')} icon={<BotIcon />} active={location.pathname.startsWith('/agents')} />
              <NavItem to="/machines" label={t('nav.machines')} icon={<ServerIcon />} active={location.pathname.startsWith('/machines')} />
              <NavItem
                to="/wiki"
                label={t('nav.wiki')}
                icon={<BookOpenIcon />}
                active={location.pathname === '/wiki' || location.pathname.startsWith('/wiki/')}
              />
              <NavItem to="/library" label={t('nav.library')} icon={<LibraryBigIcon />} active={location.pathname.startsWith('/library')} />
              <NavItem to="/usage" label={t('nav.usage')} icon={<ChartColumnIcon />} active={location.pathname.startsWith('/usage')} />
              <NavItem to="/settings" label={t('nav.settings')} icon={<SettingsIcon />} active={location.pathname.startsWith('/settings')} />
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>

        <SidebarProjects />
      </SidebarContent>

      <SidebarFooter className="px-2 pb-2">
        <UserMenu />
      </SidebarFooter>
      {creating ? <NewProjectDialog open onClose={() => setCreating(false)} /> : null}
    </Sidebar>
  )
}

interface NavItemProps {
  to: string
  label: string
  icon: React.ReactNode
  active: boolean
  count?: number
  // A count that asks for a decision is drawn in the waiting colour.
  urgent?: boolean
}

function NavItem({ to, label, icon, active, count, urgent }: NavItemProps) {
  return (
    <SidebarMenuItem>
      <SidebarMenuButton asChild isActive={active} className={cn('h-7 text-[0.8125rem]', active && 'text-foreground')}>
        <Link to={to} aria-current={active ? 'page' : undefined}>
          <span className={cn('text-subtle [&>svg]:size-4', active && 'text-foreground')}>{icon}</span>
          <span>{label}</span>
        </Link>
      </SidebarMenuButton>
      {count ? <SidebarMenuBadge className={cn('top-1', urgent ? 'text-status-wait' : 'text-subtle')}>{count}</SidebarMenuBadge> : null}
    </SidebarMenuItem>
  )
}
