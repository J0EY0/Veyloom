import { useEffect, useRef, useState } from 'react'
import { DownloadIcon, MinusIcon, PlusIcon, ScanIcon } from 'lucide-react'
import { attachmentUrl, useAttachmentText } from '@/api/attachments'
import type { Attachment } from '@/api/types'
import { CodeBlock } from '@/components/ai-elements/code-block'
import { MessageResponse } from '@/components/ai-elements/message'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { formatBytes } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { rootRem } from '@/lib/rem'
import { useNearView } from '@/lib/useInView'
import { cn } from '@/lib/utils'
import { AudioBar } from './AudioBar'
import { FileBadge } from './FileCard'
import { PdfPage } from './PdfPage'
import { isMarkdown, languageOf, withoutLastBreak } from './kinds'
import { usePdf } from './pdf'

// How much of a text file the viewer reads.
export const VIEW_TEXT = 2 * 1024 * 1024

// ViewerStage is an attachment in full, each kind its own way (docs/
// webui.md 4.21): a picture to zoom, video and sound to play, a PDF's
// pages, text whole, and for the rest a way to download it.
export function ViewerStage({ attachment }: { attachment: Attachment }) {
  switch (attachment.kind) {
    case 'image':
      return <ImageStage attachment={attachment} />
    case 'video':
      return (
        <div className="flex h-full items-center justify-center p-6">
          <video src={attachmentUrl(attachment.id)} controls autoPlay aria-label={attachment.filename} className="max-h-full max-w-full rounded-lg bg-viewer" />
        </div>
      )
    case 'audio':
      return (
        <div className="flex h-full items-center justify-center p-6">
          <AudioBar attachment={attachment} className="w-full max-w-120" />
        </div>
      )
    case 'pdf':
      return <PdfStage attachment={attachment} />
    case 'text':
      return <TextStage attachment={attachment} />
    default:
      return <NoPreview attachment={attachment} />
  }
}

const zooms = [0.25, 0.5, 0.75, 1, 1.5, 2, 3, 4]

// nextZoom is the step past a scale, in or out; past the last either way
// it stays there.
export function nextZoom(scale: number, dir: 1 | -1): number {
  if (dir > 0) return zooms.find((z) => z > scale * 1.01) ?? zooms[zooms.length - 1]
  return zooms.findLast((z) => z < scale * 0.99) ?? zooms[0]
}

function ImageStage({ attachment }: { attachment: Attachment }) {
  const t = useT()
  // 'fit' shows it whole; a number is its size in pixels times that.
  const [zoom, setZoom] = useState<'fit' | number>('fit')
  // The picture's own width, and how much of it fitting shows.
  const [natural, setNatural] = useState(attachment.width ?? 0)
  const [fitScale, setFitScale] = useState(1)
  const image = useRef<HTMLImageElement>(null)
  const fitted = zoom === 'fit'
  useEffect(() => {
    const node = image.current
    if (!fitted || !node) return
    const observer = new ResizeObserver(() => {
      if (node.naturalWidth > 0 && node.clientWidth > 0) setFitScale(node.clientWidth / node.naturalWidth)
    })
    observer.observe(node)
    return () => observer.disconnect()
  }, [fitted])
  const scale = fitted ? fitScale : zoom
  const step = (dir: 1 | -1) => setZoom((z) => nextZoom(z === 'fit' ? fitScale : z, dir))
  return (
    <div className="relative h-full">
      <div className={cn('h-full overflow-auto', fitted && 'flex items-center justify-center p-6')}>
        <img
          ref={image}
          src={attachmentUrl(attachment.id)}
          alt={attachment.filename}
          onLoad={(event) => setNatural(event.currentTarget.naturalWidth)}
          className={cn(fitted ? 'max-h-full max-w-full object-contain' : 'm-auto max-w-none')}
          style={fitted || natural === 0 ? undefined : { width: `${(natural * zoom) / rootRem()}rem` }}
        />
      </div>
      <div
        role="group"
        aria-label={t('viewer.zoom')}
        className="absolute bottom-5 left-1/2 flex h-9.5 -translate-x-1/2 items-center gap-0.5 rounded-lg bg-viewer-control px-1 text-viewer-foreground ring-1 ring-viewer-line"
      >
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={() => step(-1)}
          aria-label={t('viewer.zoomOut')}
          className="text-viewer-foreground hover:bg-viewer-hover hover:text-viewer-foreground"
        >
          <MinusIcon />
        </Button>
        <span className="w-12 text-center font-mono text-xs tabular-nums">{Math.round(scale * 100)}%</span>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={() => step(1)}
          aria-label={t('viewer.zoomIn')}
          className="text-viewer-foreground hover:bg-viewer-hover hover:text-viewer-foreground"
        >
          <PlusIcon />
        </Button>
        <span aria-hidden="true" className="mx-1 h-4.5 w-px bg-viewer-line" />
        <Button
          variant="ghost"
          size="sm"
          aria-pressed={fitted}
          onClick={() => setZoom('fit')}
          className={cn(
            'h-7.5 gap-1.5 text-xs font-normal text-viewer-foreground hover:bg-viewer-hover hover:text-viewer-foreground',
            fitted && 'bg-viewer-hover',
          )}
        >
          <ScanIcon />
          {t('viewer.fit')}
        </Button>
      </div>
    </div>
  )
}

// The widest a PDF's page is in the viewer, in rem; on a narrow screen it
// is as wide as the screen, less a margin either side.
const PDF_PAGE_REM = 50
const PDF_MARGIN_REM = 1.5

function PdfStage({ attachment }: { attachment: Attachment }) {
  const t = useT()
  const pdf = usePdf(attachmentUrl(attachment.id))
  const [scroller, setScroller] = useState<HTMLDivElement | null>(null)
  const [widthRem, setWidthRem] = useState(PDF_PAGE_REM)
  useEffect(() => {
    if (!scroller) return
    const observer = new ResizeObserver(() => {
      // In quarter rems, so a small change of size draws no page again.
      const room = Math.floor((scroller.clientWidth / rootRem() - 2 * PDF_MARGIN_REM) * 4) / 4
      if (room > 0) setWidthRem(Math.min(PDF_PAGE_REM, room))
    })
    observer.observe(scroller)
    return () => observer.disconnect()
  }, [scroller])
  if (pdf.isPending) return <Loading />
  if (pdf.isError) return <NoPreview attachment={attachment} failed />
  const pages = Array.from({ length: pdf.data.numPages }, (_, i) => i + 1)
  return (
    <div ref={setScroller} className="h-full overflow-auto">
      <div className="flex flex-col items-center gap-4 py-8">
        {/* Near is near the scroller's view, which clips the pages: the
            viewport's would find the next page out of sight. */}
        {scroller
          ? pages.map((n) => (
              <LazyPage key={n} n={n} root={scroller}>
                {(drawn) => <PdfPage doc={pdf.data} page={n} widthRem={widthRem} label={t('viewer.page', { n })} className="shadow-lg" drawn={drawn} />}
              </LazyPage>
            ))
          : null}
      </div>
    </div>
  )
}

// LazyPage draws a page while it is near the view and lets the drawing go
// once it is far, holding its place: a long PDF read through would keep a
// canvas for every page, past what a phone allows.
function LazyPage({ n, root, children }: { n: number; root: Element; children: (drawn: boolean) => React.ReactNode }) {
  const [near, setNear] = useState(n <= 2)
  const ref = useNearView<HTMLDivElement>(setNear, { root, margin: '600px' })
  return <div ref={ref}>{children(near)}</div>
}

function TextStage({ attachment }: { attachment: Attachment }) {
  const t = useT()
  const text = useAttachmentText(attachment.id, VIEW_TEXT)
  if (text.isPending) return <Loading />
  if (text.isError) return <NoPreview attachment={attachment} failed />
  return (
    <div className="h-full overflow-auto px-6 py-6">
      <div className="mx-auto w-full max-w-240">
        {text.data.more ? <p className="mb-3 text-xs text-status-wait">{t('viewer.tooBig')}</p> : null}
        {isMarkdown(attachment.filename) ? (
          <MessageResponse className="prose-agent rounded-xl border bg-card px-8 py-6">{text.data.text}</MessageResponse>
        ) : (
          <CodeBlock code={withoutLastBreak(text.data.text)} language={languageOf(attachment.filename)} showLineNumbers className="bg-card" />
        )}
      </div>
    </div>
  )
}

function Loading() {
  const t = useT()
  return (
    <p role="status" className="flex h-full items-center justify-center gap-2 text-sm text-viewer-muted">
      <Spinner className="size-4" />
      {t('common.loading')}
    </p>
  )
}

function NoPreview({ attachment, failed }: { attachment: Attachment; failed?: boolean }) {
  const t = useT()
  return (
    <div className="flex h-full flex-col items-center justify-center gap-4 p-6 text-center">
      <FileBadge attachment={attachment} className="size-20 rounded-2xl text-sm" />
      <div className="flex flex-col gap-1">
        <span className="text-sm font-medium text-viewer-foreground">{attachment.filename}</span>
        <span className="text-xs text-viewer-muted tabular-nums">{formatBytes(attachment.size)}</span>
      </div>
      <p className="text-xs text-viewer-muted">{failed ? t('attachment.readFailed') : t('viewer.noPreview')}</p>
      <Button
        asChild
        variant="outline"
        size="sm"
        className="border-viewer-line bg-transparent text-viewer-foreground hover:bg-viewer-hover hover:text-viewer-foreground"
      >
        <a href={attachmentUrl(attachment.id)} download={attachment.filename}>
          <DownloadIcon />
          {t('attachment.downloadShort')}
        </a>
      </Button>
    </div>
  )
}
