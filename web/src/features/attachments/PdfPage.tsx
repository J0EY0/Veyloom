import { useEffect, useRef, useState } from 'react'
import type { PDFDocumentProxy, RenderTask } from 'pdfjs-dist'
import { rootRem } from '@/lib/rem'
import { cn } from '@/lib/utils'

export interface PdfPageProps {
  doc: PDFDocumentProxy
  // Pages count from 1.
  page: number
  // How wide the page is drawn, in rem; its height follows its shape.
  widthRem: number
  label: string
  className?: string
  // drawn false holds the page's place undrawn, letting go of what it drew.
  drawn?: boolean
}

// PdfPage draws one page of a PDF as wide as asked, sharp on a dense
// screen. Until its shape is known it holds the shape of an A4 page.
export function PdfPage({ doc, page, widthRem, label, className, drawn = true }: PdfPageProps) {
  const canvas = useRef<HTMLCanvasElement>(null)
  const [ratio, setRatio] = useState(1.414)
  useEffect(() => {
    if (!drawn) {
      // A canvas keeps a bitmap as big as the page it drew.
      const node = canvas.current
      if (node) {
        node.width = 0
        node.height = 0
      }
      return
    }
    let task: RenderTask | undefined
    let gone = false
    void doc
      .getPage(page)
      .then((p) => {
        const node = canvas.current
        if (gone || !node) return
        const base = p.getViewport({ scale: 1 })
        setRatio(base.height / base.width)
        const viewport = p.getViewport({ scale: (widthRem * rootRem() * (window.devicePixelRatio || 1)) / base.width })
        node.width = Math.floor(viewport.width)
        node.height = Math.floor(viewport.height)
        task = p.render({ canvas: node, viewport })
        return task.promise
      })
      // A page that cannot be drawn stays blank; a cancelled one is gone.
      .catch(() => undefined)
    return () => {
      gone = true
      task?.cancel()
    }
  }, [doc, page, widthRem, drawn])
  return (
    <canvas
      ref={canvas}
      role={drawn ? 'img' : undefined}
      aria-label={drawn ? label : undefined}
      className={cn('block', drawn ? 'bg-paper' : 'bg-viewer-control', className)}
      style={{ width: `${widthRem}rem`, height: `${widthRem * ratio}rem` }}
    />
  )
}
