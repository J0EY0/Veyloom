import { BotIcon, ChevronDownIcon, ServerIcon } from 'lucide-react'
import { useNavigate } from 'react-router'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useT } from '@/lib/i18n'
import { openPalette } from '@/lib/palette'

// The workspace name at the top of the sidebar, and the menu behind it:
// the places that are not a room.
export function WorkspaceMenu() {
  const navigate = useNavigate()
  const t = useT()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          aria-label={t('nav.workspace')}
          className="gap-2 px-1.5 font-semibold hover:bg-sidebar-accent data-open:bg-sidebar-accent"
        >
          <span
            aria-hidden="true"
            className="inline-flex size-4.5 items-center justify-center rounded-[5px] bg-primary text-[0.625rem] font-bold text-primary-foreground"
          >
            V
          </span>
          Veyloom
          <ChevronDownIcon className="text-subtle" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-52">
        <DropdownMenuLabel className="text-xs text-subtle">Veyloom</DropdownMenuLabel>
        <DropdownMenuItem onSelect={() => void navigate('/agents')}>
          <BotIcon />
          {t('nav.agents')}
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => void navigate('/machines')}>
          <ServerIcon />
          {t('nav.machines')}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={openPalette}>
          {t('common.search')}
          <DropdownMenuShortcut>⌘K</DropdownMenuShortcut>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
