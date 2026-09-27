import { useEffect } from 'react'
import { useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import type { PDFDocumentProxy, PDFWorker } from 'pdfjs-dist'
import { settledMeta } from '@/api/refresh'

// PDFs are drawn with pdf.js (docs/webui.md 4.21), loaded the first time a
// PDF comes into view: a chat without one downloads none of it. A PDF is
// read in ranges as its pages are drawn, so the first page of a long one
// does not wait for the whole file. It is never opened as a page, so the
// attachment's sandbox does not matter, and pdf.js 6 runs nothing a PDF
// carries: it has no eval path, and XFA forms stay off.

type PdfJs = typeof import('pdfjs-dist')

let loading: Promise<{ lib: PdfJs; worker: PDFWorker }> | null = null

// pdfjs loads pdf.js and the one worker every PDF is read in: left to
// itself, pdf.js starts a worker for each document, and a room of PDFs
// would start as many.
function pdfjs(): Promise<{ lib: PdfJs; worker: PDFWorker }> {
  loading ??= Promise.all([import('pdfjs-dist'), import('pdfjs-dist/build/pdf.worker.min.mjs?url')]).then(([lib, src]) => {
    lib.GlobalWorkerOptions.workerSrc = src.default
    return { lib, worker: new lib.PDFWorker() }
  })
  return loading
}

export async function openPdf(url: string): Promise<PDFDocumentProxy> {
  const { lib, worker } = await pdfjs()
  return lib.getDocument({ url, worker, withCredentials: true, disableAutoFetch: true, disableStream: true }).promise
}

// closing watches a query cache for the PDFs it lets go of, once a while
// after nothing shows them, and closes them: what the worker holds of
// them goes with them.
const closing = new WeakSet<QueryClient>()

function closeWhenDropped(client: QueryClient) {
  if (closing.has(client)) return
  closing.add(client)
  client.getQueryCache().subscribe((event) => {
    if (event.type === 'removed' && event.query.queryKey[0] === 'pdf') {
      void (event.query.state.data as PDFDocumentProxy | undefined)?.loadingTask.destroy()
    }
  })
}

// usePdf opens a PDF once for every place that shows it. An attachment does
// not change, so it is never read again (settledMeta).
export function usePdf(url: string, enabled = true) {
  const client = useQueryClient()
  useEffect(() => closeWhenDropped(client), [client])
  return useQuery({
    queryKey: ['pdf', url],
    queryFn: () => openPdf(url),
    staleTime: Infinity,
    gcTime: 5 * 60_000,
    enabled,
    retry: false,
    meta: settledMeta,
  })
}
