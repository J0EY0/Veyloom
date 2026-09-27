import type { PDFDocumentProxy } from 'pdfjs-dist'

// fakePdf stands in for a PDF pdf.js has opened: letter-sized pages that
// draw nothing.
export function fakePdf(numPages: number): PDFDocumentProxy {
  const page = {
    getViewport: ({ scale }: { scale: number }) => ({ width: 612 * scale, height: 792 * scale }),
    render: () => ({ promise: Promise.resolve(), cancel: () => {} }),
  }
  return { numPages, getPage: () => Promise.resolve(page) } as unknown as PDFDocumentProxy
}
