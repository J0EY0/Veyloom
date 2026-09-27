import { useState } from 'react'
import { attachmentUrl } from '@/api/attachments'
import type { Attachment } from '@/api/types'
import { Skeleton } from '@/components/ui/skeleton'
import { formatBytes } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { useInView } from '@/lib/useInView'
import { cn } from '@/lib/utils'
import { DownloadButton, FileBadge } from './FileCard'
import { PdfPage } from './PdfPage'
import { usePdf } from './pdf'

// How wide a PDF's first page is in the chat, in rem: about as wide as a
// picture there.
const PAGE_REM = 15

// PdfPreview is a PDF where it was sent (docs/webui.md 4.21): its first
// page, as large as a picture, then its name, pages and size. pdf.js and
// the file load once it scrolls into view.
export function PdfPreview({ attachment, onOpen, className }: { attachment: Attachment; onOpen: () => void; className?: string }) {
  const t = useT()
  const [seen, setSeen] = useState(false)
  const ref = useInView<HTMLButtonElement>(() => setSeen(true))
  const pdf = usePdf(attachmentUrl(attachment.id), seen)
  const pages = pdf.data?.numPages

  return (
    <div className={cn('flex w-fit max-w-full flex-col overflow-hidden rounded-xl border bg-card', className)}>
      <button
        ref={ref}
        type="button"
        onClick={onOpen}
        aria-label={t('attachment.preview', { name: attachment.filename })}
        className="flex cursor-zoom-in justify-center bg-muted/40 px-6 pt-5 pb-5 outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
      >
        {pdf.data ? (
          <PdfPage
            doc={pdf.data}
            page={1}
            widthRem={PAGE_REM}
            label={t('attachment.firstPage', { name: attachment.filename })}
            className="rounded-sm shadow-md"
          />
        ) : pdf.isError ? (
          <span className="flex h-84 w-60 items-center justify-center rounded-sm bg-muted text-xs text-subtle">{t('attachment.readFailed')}</span>
        ) : (
          <Skeleton className="h-84 w-60 rounded-sm" />
        )}
      </button>
      <div className="flex items-center gap-2.5 border-t py-2 pr-1.5 pl-3">
        <FileBadge attachment={attachment} className="size-7.5 text-[0.5625rem]" />
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="truncate text-[0.8125rem] font-medium text-foreground">{attachment.filename}</span>
          <span className="text-xs text-subtle tabular-nums">
            {pages ? `${t('attachment.pages', { n: pages })} · ` : ''}
            {formatBytes(attachment.size)}
          </span>
        </span>
        <DownloadButton attachment={attachment} />
      </div>
    </div>
  )
}
