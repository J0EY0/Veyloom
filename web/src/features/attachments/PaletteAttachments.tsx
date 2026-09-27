import { useEffect, useState } from 'react'
import { ArrowRightIcon } from 'lucide-react'
import { useMatch } from 'react-router'
import { useRoomAttachments } from '@/api/attachments'
import { useProject } from '@/api/projects'
import { useRoom } from '@/api/rooms'
import type { RoomAttachment } from '@/api/types'
import { CommandGroup, CommandItem } from '@/components/ui/command'
import { formatBytes, formatCount } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { FileBadge } from './FileCard'

// How many attachments the search lists before "see them all", and how
// long typing rests before they are looked for.
const SHOWN = 5
const TYPING_MS = 250

export interface PaletteHits {
  roomId: string
  // The words looked for, once typing rested.
  q: string
  attachments: RoomAttachment[]
  total: number
  // The project whose chat is open.
  project: string
}

// usePaletteAttachments looks for the search's words among the attachments
// of the project whose chat is open (docs/webui.md 4.21), with the
// attachments tab's own list; anywhere else there is nothing to look in.
export function usePaletteAttachments(search: string): PaletteHits {
  const roomId = useMatch('/rooms/:roomId/*')?.params.roomId ?? ''
  const room = useRoom(roomId)
  const project = useProject(room.data?.project_id ?? '')
  const [q, setQ] = useState(search.trim())
  useEffect(() => {
    const timer = setTimeout(() => setQ(search.trim()), TYPING_MS)
    return () => clearTimeout(timer)
  }, [search])
  const looking = roomId !== '' && q !== ''
  const list = useRoomAttachments(roomId, { q, kinds: [], sender: '', sort: 'newest' }, looking)
  const page = looking ? list.data?.pages[0] : undefined
  return { roomId, q, attachments: page?.attachments.slice(0, SHOWN) ?? [], total: page?.total ?? 0, project: project?.name ?? room.data?.name ?? '' }
}

export interface PaletteAttachmentsProps {
  hits: PaletteHits
  onOpen: (attachment: RoomAttachment) => void
  onSeeAll: () => void
}

// PaletteAttachments is the search's group of attachments: each opens in
// the viewer, and when there are more than it lists, the last item opens
// the attachments tab with the words. cmdk does not filter them; the hub
// already has.
export function PaletteAttachments({ hits, onOpen, onSeeAll }: PaletteAttachmentsProps) {
  const t = useT()
  if (hits.attachments.length === 0) return null
  return (
    <CommandGroup heading={t('palette.attachments', { project: hits.project })} forceMount>
      {hits.attachments.map((attachment) => (
        <CommandItem key={attachment.id} value={`attachment ${attachment.id}`} forceMount onSelect={() => onOpen(attachment)}>
          <FileBadge attachment={attachment} className="size-6 rounded-md text-[0.5rem]" />
          <span className="min-w-0 flex-1 truncate">{attachment.filename}</span>
          <span className="flex-none text-xs text-subtle tabular-nums">
            {attachment.sender_name} · {formatBytes(attachment.size)}
          </span>
        </CommandItem>
      ))}
      {hits.total > hits.attachments.length ? (
        <CommandItem value="attachment see all" forceMount onSelect={onSeeAll}>
          <ArrowRightIcon className="text-subtle" />
          {t('palette.allAttachments', { n: formatCount(hits.total) })}
        </CommandItem>
      ) : null}
    </CommandGroup>
  )
}
