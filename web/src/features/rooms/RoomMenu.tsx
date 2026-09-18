import { EllipsisIcon, LinkIcon, ShieldCheckIcon, UsersIcon } from 'lucide-react'
import { useNavigate } from 'react-router'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { copyText } from '@/lib/clipboard'
import { useT } from '@/lib/i18n'

export interface RoomMenuProps {
  roomId: string
}

// The "…" next to the chat's name: the things done to a chat that are not
// said in it.
export function RoomMenu({ roomId }: RoomMenuProps) {
  const navigate = useNavigate()
  const t = useT()

  async function copyLink() {
    if (await copyText(location.href)) toast.success(t('common.linkCopied'))
    else toast.error(t('common.copyFailed'))
  }

  return (
    <>
      <DropdownMenu>
        <Tooltip>
          <TooltipTrigger asChild>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon-sm" aria-label={t('room.menu')} className="text-subtle hover:text-foreground">
                <EllipsisIcon />
              </Button>
            </DropdownMenuTrigger>
          </TooltipTrigger>
          <TooltipContent side="bottom">{t('common.more')}</TooltipContent>
        </Tooltip>
        <DropdownMenuContent align="start" className="w-48">
          <DropdownMenuItem onSelect={() => void navigate(`/rooms/${roomId}?panel=members`)}>
            <UsersIcon />
            {t('room.info')}
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => void navigate(`/rooms/${roomId}?panel=approvals`)}>
            <ShieldCheckIcon />
            {t('nav.approvals')}
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => void copyLink()}>
            <LinkIcon />
            {t('common.copyLink')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </>
  )
}
