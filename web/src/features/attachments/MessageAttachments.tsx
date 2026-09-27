import { Maximize2Icon, PlayIcon } from 'lucide-react'
import { attachmentUrl, pictureUrl } from '@/api/attachments'
import type { Attachment } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { AudioBar } from './AudioBar'
import { FileCard } from './FileCard'
import { PdfPreview } from './PdfPreview'
import { TextPreview } from './TextPreview'
import { openViewer } from './viewerStore'

export interface MessageAttachmentsProps {
  attachments: Attachment[] | null | undefined
  roomId: string
  // The topic the message is in; absent for the room itself.
  threadId?: string
  className?: string
}

// MessageAttachments draws what a message carries right in the chat (docs/
// webui.md 4.21): pictures and video first, a single one at its own shape
// and several in a grid; then sound to play, text to read, a PDF's first
// page, and cards for the rest. Any of them opens the viewer.
export function MessageAttachments({ attachments, roomId, threadId, className }: MessageAttachmentsProps) {
  const t = useT()
  if (!attachments?.length) return null
  const open = (attachment: Attachment) => openViewer({ roomId, attachment, threadId })
  const media = attachments.filter(isMedia)
  const rest = attachments.filter((a) => !isMedia(a))
  return (
    <div role="group" aria-label={t('attachment.list')} className={cn('mt-2 flex min-w-0 flex-col items-start gap-2', className)}>
      {media.length === 1 ? (
        media[0].kind === 'video' ? (
          <InlineVideo attachment={media[0]} onOpen={() => open(media[0])} />
        ) : (
          <Picture attachment={media[0]} onOpen={() => open(media[0])} />
        )
      ) : media.length > 1 ? (
        <div className={cn('grid max-w-full gap-1', media.length === 2 || media.length === 4 ? 'w-61 grid-cols-2' : 'w-92 grid-cols-3')}>
          {media.map((a) => (
            <Tile key={a.id} attachment={a} onOpen={() => open(a)} />
          ))}
        </div>
      ) : null}
      {rest.map((a) =>
        a.kind === 'audio' ? (
          <AudioBar key={a.id} attachment={a} />
        ) : a.kind === 'text' ? (
          <TextPreview key={a.id} attachment={a} onOpen={() => open(a)} />
        ) : a.kind === 'pdf' ? (
          <PdfPreview key={a.id} attachment={a} onOpen={() => open(a)} />
        ) : (
          <FileCard key={a.id} attachment={a} onOpen={() => open(a)} />
        ),
      )}
    </div>
  )
}

function isMedia(a: Attachment): boolean {
  return a.kind === 'image' || a.kind === 'video'
}

// The largest a single picture is in the chat, either way, in rem.
const MAX_REM = 20
const REM_PX = 16

// fit is the box a picture is shown in: its own shape, no bigger than it
// is nor than MAX_REM either way. Without its size, a 16:10 box.
export function fit(width = 0, height = 0): { w: number; h: number } {
  if (width <= 0 || height <= 0) return { w: 16, h: 10 }
  const ratio = width / height
  let w = Math.min(MAX_REM, width / REM_PX)
  let h = w / ratio
  if (h > MAX_REM) {
    h = MAX_REM
    w = h * ratio
  }
  return { w: Math.max(w, 2), h: Math.max(h, 2) }
}

function Picture({ attachment, onOpen }: { attachment: Attachment; onOpen: () => void }) {
  const t = useT()
  const box = fit(attachment.width, attachment.height)
  return (
    <button
      type="button"
      onClick={onOpen}
      aria-label={t('attachment.preview', { name: attachment.filename })}
      className="block max-w-full cursor-zoom-in overflow-hidden rounded-xl border bg-muted outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
      style={{ width: `${box.w}rem`, aspectRatio: `${box.w} / ${box.h}` }}
    >
      <img src={pictureUrl(attachment)} alt="" loading="lazy" decoding="async" className="size-full object-cover" />
    </button>
  )
}

// Tile is one of several pictures or videos: a square, cropped, 7.5rem
// or less on a narrow screen. Two or four go two to a row.
function Tile({ attachment, onOpen }: { attachment: Attachment; onOpen: () => void }) {
  const t = useT()
  return (
    <button
      type="button"
      onClick={onOpen}
      aria-label={t('attachment.preview', { name: attachment.filename })}
      className="relative block aspect-square w-full cursor-zoom-in overflow-hidden rounded-lg border bg-muted outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
    >
      {attachment.kind === 'video' ? (
        <>
          <video src={attachmentUrl(attachment.id)} preload="metadata" muted className="size-full object-cover" />
          <PlayMark />
        </>
      ) : (
        <img src={pictureUrl(attachment)} alt="" loading="lazy" decoding="async" className="size-full object-cover" />
      )}
    </button>
  )
}

export function PlayMark({ className }: { className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        'absolute top-1/2 left-1/2 flex size-9 -translate-1/2 items-center justify-center rounded-full bg-viewer-control text-viewer-foreground ring-1 ring-viewer-line',
        className,
      )}
    >
      <PlayIcon className="size-3.5 translate-x-px fill-current" />
    </span>
  )
}

// InlineVideo plays a video where it was sent, 16:9, with a way to the
// viewer.
function InlineVideo({ attachment, onOpen }: { attachment: Attachment; onOpen: () => void }) {
  const t = useT()
  return (
    <div className="relative w-96 max-w-full overflow-hidden rounded-xl border bg-viewer">
      <video
        src={attachmentUrl(attachment.id)}
        controls
        preload="metadata"
        aria-label={attachment.filename}
        className="block aspect-video w-full object-contain"
      />
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={onOpen}
            aria-label={t('attachment.preview', { name: attachment.filename })}
            className="absolute top-2 right-2 bg-viewer-control text-viewer-foreground hover:bg-viewer-hover hover:text-viewer-foreground"
          >
            <Maximize2Icon />
          </Button>
        </TooltipTrigger>
        <TooltipContent>{t('attachment.previewShort')}</TooltipContent>
      </Tooltip>
    </div>
  )
}
