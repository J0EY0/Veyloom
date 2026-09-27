import { DownloadIcon } from 'lucide-react'
import { attachmentUrl } from '@/api/attachments'
import type { Attachment } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { formatBytes } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { extensionOf } from './kinds'

// FileBadge is a file's mark: its extension in a small square.
export function FileBadge({ attachment, className }: { attachment: Pick<Attachment, 'filename' | 'kind'>; className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        'flex size-9 flex-none items-center justify-center rounded-lg bg-muted font-mono text-[0.625rem] font-medium tracking-wide text-muted-foreground ring-1 ring-border ring-inset',
        className,
      )}
    >
      {extensionOf(attachment.filename, attachment.kind)}
    </span>
  )
}

// DownloadButton saves an attachment under its own name.
export function DownloadButton({ attachment, className }: { attachment: Pick<Attachment, 'id' | 'filename'>; className?: string }) {
  const t = useT()
  const label = t('attachment.download', { name: attachment.filename })
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button asChild variant="ghost" size="icon-sm" className={cn('flex-none text-subtle hover:text-foreground', className)}>
          <a href={attachmentUrl(attachment.id)} download={attachment.filename} aria-label={label}>
            <DownloadIcon />
          </a>
        </Button>
      </TooltipTrigger>
      <TooltipContent>{t('attachment.downloadShort')}</TooltipContent>
    </Tooltip>
  )
}

export interface FileCardProps {
  attachment: Attachment
  // What is said of it after its size: its pages, lines, length.
  detail?: string
  onOpen: () => void
  className?: string
}

// FileCard is a file the chat has no picture of: its mark, name and size,
// opening the viewer; the download beside it.
export function FileCard({ attachment, detail, onOpen, className }: FileCardProps) {
  const t = useT()
  return (
    <div className={cn('flex w-75 max-w-full items-center gap-3 rounded-xl border bg-card py-2 pr-1.5 pl-2.5', className)}>
      <FileBadge attachment={attachment} />
      <button
        type="button"
        onClick={onOpen}
        aria-label={t('attachment.preview', { name: attachment.filename })}
        className="flex min-w-0 flex-1 flex-col items-start gap-0.5 rounded-md text-left outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
      >
        <span className="w-full truncate text-[0.8125rem] font-medium text-foreground">{attachment.filename}</span>
        <span className="text-xs text-subtle tabular-nums">
          {detail ? `${detail} · ` : ''}
          {formatBytes(attachment.size)}
        </span>
      </button>
      <DownloadButton attachment={attachment} />
    </div>
  )
}
