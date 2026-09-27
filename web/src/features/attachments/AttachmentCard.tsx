import { useState } from 'react'
import { FileIcon, PlayIcon } from 'lucide-react'
import { attachmentUrl, pictureUrl } from '@/api/attachments'
import type { RoomAttachment } from '@/api/types'
import { Checkbox } from '@/components/ui/checkbox'
import { Skeleton } from '@/components/ui/skeleton'
import { formatBytes } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { useInView } from '@/lib/useInView'
import { cn } from '@/lib/utils'
import { clock } from './AudioBar'
import { extensionOf } from './kinds'
import { PlayMark } from './MessageAttachments'
import { PdfPage } from './PdfPage'
import { usePdf } from './pdf'

export interface AttachmentCardProps {
  attachment: RoomAttachment
  // When it was sent, as the list says it.
  when: string
  // Picking files to download together: a press picks the card instead of
  // opening it.
  picking: boolean
  picked: boolean
  onOpen: () => void
  onPick: () => void
}

// AttachmentCard is one attachment on the attachments tab (docs/webui.md
// 4.21, the refined cards): a 4:3 look at it, no frame and no buttons, its
// name, and who sent it when and how big it is. Downloading and finding it
// in the chat are in the viewer.
export function AttachmentCard({ attachment, when, picking, picked, onOpen, onPick }: AttachmentCardProps) {
  const t = useT()
  // A PDF's pages are known once its first page has been drawn.
  const pdf = usePdf(attachmentUrl(attachment.id), false)
  const pages = attachment.kind === 'pdf' ? pdf.data?.numPages : undefined
  const meta = [attachment.sender_name, when, pages ? t('attachment.pages', { n: pages }) : undefined, formatBytes(attachment.size)]
  const media = attachment.kind === 'image' || attachment.kind === 'video'
  return (
    <article className="flex min-w-0 flex-col gap-2">
      <div className={cn('relative rounded-xl', picked && 'ring-2 ring-foreground/80 ring-offset-2 ring-offset-background')}>
        <button
          type="button"
          onClick={picking ? onPick : onOpen}
          aria-pressed={picking ? picked : undefined}
          aria-label={picking ? t('attachments.pick', { name: attachment.filename }) : t('attachment.preview', { name: attachment.filename })}
          className={cn(
            'relative block aspect-4/3 w-full overflow-hidden rounded-xl outline-none focus-visible:ring-2 focus-visible:ring-ring/50',
            picking ? 'cursor-pointer' : 'cursor-zoom-in',
            media ? 'bg-muted' : 'bg-card ring-1 ring-border ring-inset',
          )}
        >
          <Look attachment={attachment} />
        </button>
        {picking ? (
          <span aria-hidden="true" className="pointer-events-none absolute top-2.5 left-2.5 flex size-6.5 items-center justify-center rounded-md bg-viewer/70">
            <Checkbox checked={picked} tabIndex={-1} className="border-viewer-foreground/70" />
          </span>
        ) : null}
      </div>
      <div className="flex min-w-0 flex-col gap-0.5 px-0.5">
        <span className="truncate text-[0.8125rem] text-foreground">{attachment.filename}</span>
        <span className="truncate text-xs text-subtle tabular-nums">{meta.filter(Boolean).join(' · ')}</span>
      </div>
    </article>
  )
}

function Look({ attachment }: { attachment: RoomAttachment }) {
  switch (attachment.kind) {
    case 'image':
      return <img src={pictureUrl(attachment)} alt="" loading="lazy" decoding="async" className="size-full object-cover" />
    case 'video':
      return <VideoLook attachment={attachment} />
    case 'pdf':
      return <PdfLook attachment={attachment} />
    case 'audio':
      return <AudioLook attachment={attachment} />
    default:
      return <FileLook attachment={attachment} />
  }
}

// A plain document and the file's extension, nothing written small.
function FileLook({ attachment }: { attachment: RoomAttachment }) {
  return (
    <span className="flex size-full flex-col items-center justify-center gap-2.5">
      <FileIcon aria-hidden="true" strokeWidth={1} className="size-14 fill-muted text-muted-foreground/60" />
      <span className="font-mono text-[0.6875rem] tracking-wider text-muted-foreground">{extensionOf(attachment.filename, attachment.kind)}</span>
    </span>
  )
}

// useSeen says a card has come near the view; what it shows of a video, a
// sound or a PDF loads only then.
function useSeen() {
  const [seen, setSeen] = useState(false)
  const ref = useInView<HTMLSpanElement>(() => setSeen(true))
  return [ref, seen] as const
}

// A video's first frame, a play mark, and its length once known.
function VideoLook({ attachment }: { attachment: RoomAttachment }) {
  const [ref, seen] = useSeen()
  const [length, setLength] = useState(0)
  return (
    <span ref={ref} className="block size-full">
      {seen ? (
        <video
          src={attachmentUrl(attachment.id)}
          preload="metadata"
          muted
          playsInline
          onLoadedMetadata={(event) => setLength(event.currentTarget.duration)}
          className="size-full object-cover"
        />
      ) : null}
      <PlayMark />
      {length > 0 ? (
        <span className="absolute right-2 bottom-2 rounded-md bg-viewer/70 px-1.5 py-0.5 font-mono text-[0.6875rem] text-viewer-foreground tabular-nums">
          {clock(length)}
        </span>
      ) : null}
    </span>
  )
}

// A sound's play mark and its length once known.
function AudioLook({ attachment }: { attachment: RoomAttachment }) {
  const [ref, seen] = useSeen()
  const [length, setLength] = useState(0)
  return (
    <span ref={ref} className="flex size-full flex-col items-center justify-center gap-3">
      <span className="flex size-11 items-center justify-center rounded-full bg-foreground text-background">
        <PlayIcon aria-hidden="true" className="size-4 translate-x-px fill-current" />
      </span>
      <span className="h-4 font-mono text-xs text-muted-foreground tabular-nums">{length > 0 ? clock(length) : ''}</span>
      {seen ? (
        <audio src={attachmentUrl(attachment.id)} preload="metadata" onLoadedMetadata={(event) => setLength(event.currentTarget.duration)} className="hidden" />
      ) : null}
    </span>
  )
}

// The top of a PDF's first page.
function PdfLook({ attachment }: { attachment: RoomAttachment }) {
  const t = useT()
  const [ref, seen] = useSeen()
  const pdf = usePdf(attachmentUrl(attachment.id), seen)
  if (pdf.isError) return <FileLook attachment={attachment} />
  return (
    <span ref={ref} className="flex size-full justify-center pt-4">
      {pdf.data ? (
        <PdfPage doc={pdf.data} page={1} widthRem={6.625} label={t('attachment.firstPage', { name: attachment.filename })} className="rounded-xs shadow-sm" />
      ) : (
        <Skeleton className="h-37.5 w-26.5 rounded-xs" />
      )}
    </span>
  )
}
