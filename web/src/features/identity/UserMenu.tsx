import { ChevronsUpDownIcon, LogOutIcon, SettingsIcon } from 'lucide-react'
import { useNavigate } from 'react-router'
import { useLogout } from '@/api/auth'
import { UserAvatar } from '@/components/shared/user-avatar'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { SidebarMenuButton } from '@/components/ui/sidebar'
import { useCurrentUser } from '@/lib/currentUser'
import { useT } from '@/lib/i18n'

// The sidebar's bottom row: the signed-in account, with the way to the
// settings page and out (docs/webui.md §4.8).
export function UserMenu() {
  const user = useCurrentUser()
  const logout = useLogout()
  const navigate = useNavigate()
  const t = useT()

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <SidebarMenuButton
          size="lg"
          aria-label={user ? t('user.current', { name: user.name }) : t('user.loading')}
          className="h-10 data-open:bg-sidebar-accent"
        >
          <UserAvatar name={user?.name ?? '?'} />
          <span className="min-w-0 flex-1 truncate text-[0.8125rem] font-medium text-foreground">{user?.name ?? t('user.loading')}</span>
          <ChevronsUpDownIcon className="ml-auto size-3.5 text-subtle" />
        </SidebarMenuButton>
      </DropdownMenuTrigger>
      <DropdownMenuContent side="top" align="start" className="w-(--radix-dropdown-menu-trigger-width) min-w-56">
        <DropdownMenuLabel className="text-xs text-subtle">{t('user.accountTitle')}</DropdownMenuLabel>
        <DropdownMenuItem onSelect={() => void navigate('/settings')}>
          <SettingsIcon />
          {t('nav.settings')}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem disabled={logout.isPending} onSelect={() => logout.mutate()}>
          <LogOutIcon />
          {t('user.logout')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
