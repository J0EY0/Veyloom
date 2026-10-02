import { useEffect, useMemo, useRef, useState, useSyncExternalStore, type KeyboardEvent } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { ChevronLeftIcon, ChevronRightIcon, CopyIcon, MessageSquareIcon, XIcon } from 'lucide-react'
import { useNavigate } from 'react-router'
import { toast } from 'sonner'
import { attachmentKeys, fetchText, useRoomAttachments, type AttachmentFilter } from '@/api/attachments'
import type { Attachment, RoomAttachment } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { copyText } from '@/lib/clipboard'
import { formatBytes, formatTime } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { DownloadButton, FileBadge } from './FileCard'
import { ViewerStage, VIEW_TEXT } from './ViewerStage'
import { closeViewer, useViewerTarget, type ViewerTarget } from './viewerStore'

// The header's buttons with words; on a phone only their icons.
const labelled = 'border-viewer-line bg-transparent text-viewer-foreground hover:bg-viewer-hover hover:text-viewer-foreground max-sm:px-2'

// A phone's width, Tailwind's below sm, where the header keeps only icons.
const phoneQuery = '(max-width: 39.9375rem)'

// usePhone says whether the viewer is on a phone's width, as it changes.
function usePhone(): boolean {
  return useSyncExternalStore(
    (onChange) => {
      const list = window.matchMedia(phoneQuery)
      list.addEventListener('change', onChange)
      return () => list.removeEventListener('change', onChange)
    },
    () => window.matchMedia(phoneQuery).matches,
    () => false,
  )
}

// Every attachment of the room, loaded the newest first: the ones people
// open most are on the first page.
const everything: AttachmentFilter = { q: '', kinds: [], sender: '', sort: 'newest' }

// AttachmentViewer is the full-screen viewer (docs/webui.md 4.21), opened
// from a message, the attachments tab or the search. The arrows, and the
// arrow keys, step through the attachments of the room: from the tab in
// its order, from anywhere else from the oldest on the left to the newest
// on the right, as the chat reads. On a phone the arrows are in a bar
// under the attachment, with where it is among them: over its sides they
// covered a sound's play button and a video's.
export function AttachmentViewer() {
  const target = useViewerTarget()
  if (!target) return null
  return <Viewer key={`${target.roomId}/${target.attachment.id}`} target={target} />
}

function Viewer({ target }: { target: ViewerTarget }) {
  const t = useT()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const phone = usePhone()
  const content = useRef<HTMLDivElement>(null)
  const [current, setCurrent] = useState<Attachment>(target.attachment)
  const inTabOrder = target.filter !== undefined
  const list = useRoomAttachments(target.roomId, target.filter ?? everything)
  const all = useMemo(() => list.data?.pages.flatMap((page) => page.attachments) ?? [], [list.data])
  const index = all.findIndex((a) => a.id === current.id)
  const total = list.data?.pages[0]?.total ?? 0
  const found = index >= 0
  const info: RoomAttachment | undefined = found ? all[index] : undefined
  const previous = !found ? undefined : inTabOrder ? all[index - 1] : all[index + 1]
  const next = !found ? undefined : inTabOrder ? all[index + 1] : all[index - 1]
  const position = inTabOrder ? index + 1 : total - index

  // Page on until the one shown is among those loaded, and keep a page
  // ahead of it so the arrow toward the end of the list always has
  // somewhere to go. A page that would not load is not asked for again
  // and again: the room gone, say, it never will.
  const { hasNextPage, isFetchingNextPage, isFetchNextPageError, fetchNextPage } = list
  useEffect(() => {
    if ((index < 0 || index >= all.length - 3) && hasNextPage && !isFetchingNextPage && !isFetchNextPageError) void fetchNextPage()
  }, [index, all.length, hasNextPage, isFetchingNextPage, isFetchNextPageError, fetchNextPage])

  function onKeyDown(event: KeyboardEvent) {
    // A slider, a text box and a playing video use the arrows themselves.
    const at = event.target
    if (at instanceof HTMLInputElement || at instanceof HTMLTextAreaElement || at instanceof HTMLMediaElement) return
    if (event.key === 'ArrowLeft' && previous) setCurrent(previous)
    if (event.key === 'ArrowRight' && next) setCurrent(next)
  }

  // Back to the message: its topic, or its place in the chat.
  function showInChat() {
    const threadId = info?.thread_id ?? (current.id === target.attachment.id ? target.threadId : undefined)
    const messageId = current.message_id ?? info?.message_id
    closeViewer()
    if (threadId) void navigate(`/rooms/${target.roomId}?thread=${threadId}`)
    else if (messageId) void navigate(`/rooms/${target.roomId}?message=${messageId}`)
  }

  // What the stage read is what is copied, read again only if it had not.
  async function copy() {
    const text = await queryClient.ensureQueryData({ queryKey: attachmentKeys.text(current.id, VIEW_TEXT), queryFn: () => fetchText(current.id, VIEW_TEXT) })
    if (await copyText(text.text)) toast.success(t('viewer.copied'))
  }

  const meta = [
    info?.sender_name,
    formatTime(current.created_at),
    current.width && current.height ? `${current.width} × ${current.height}` : undefined,
    formatBytes(current.size),
  ].filter(Boolean)

  return (
    <Dialog open onOpenChange={(open) => !open && closeViewer()}>
      <DialogContent
        ref={content}
        showCloseButton={false}
        onKeyDown={onKeyDown}
        // The viewer takes the keys, not its first button.
        onOpenAutoFocus={(event) => {
          event.preventDefault()
          content.current?.focus()
        }}
        className="flex h-dvh w-screen max-w-none flex-col gap-0 rounded-none border-0 bg-viewer p-0 text-viewer-foreground shadow-none sm:max-w-none"
      >
        <header className="flex h-15 flex-none items-center gap-3 border-b border-viewer-line pr-3.5 pl-5 max-sm:gap-1 max-sm:pr-2 max-sm:pl-4">
          <FileBadge attachment={current} className="size-7.5 bg-viewer-control text-viewer-muted ring-viewer-line max-sm:hidden" />
          {/* On a phone the name takes what the buttons leave. */}
          <div className="flex min-w-0 flex-col max-sm:flex-1">
            <DialogTitle className="truncate text-sm font-semibold text-viewer-foreground">{current.filename}</DialogTitle>
            <DialogDescription className="truncate text-xs text-viewer-muted tabular-nums">{meta.join(' · ')}</DialogDescription>
          </div>
          <span className="grow max-sm:hidden" />
          {found && total > 0 ? (
            <span className="font-mono text-[0.78125rem] text-viewer-muted tabular-nums max-sm:hidden">
              {position} / {total}
            </span>
          ) : null}
          <span className="grow max-sm:hidden" />
          {current.kind === 'text' ? (
            <Button variant="outline" size="sm" onClick={() => void copy()} className={labelled}>
              <CopyIcon />
              <span className="max-sm:sr-only">{t('viewer.copy')}</span>
            </Button>
          ) : null}
          {current.message_id || info ? (
            <Button variant="outline" size="sm" onClick={showInChat} className={labelled}>
              <MessageSquareIcon />
              <span className="max-sm:sr-only">{t('viewer.inChat')}</span>
            </Button>
          ) : null}
          <DownloadButton attachment={current} className="text-viewer-muted hover:bg-viewer-hover hover:text-viewer-foreground" />
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={closeViewer}
                aria-label={t('viewer.close')}
                className="text-viewer-muted hover:bg-viewer-hover hover:text-viewer-foreground"
              >
                <XIcon />
              </Button>
            </TooltipTrigger>
            <TooltipContent>{t('viewer.close')}</TooltipContent>
          </Tooltip>
        </header>
        <div className="relative min-h-0 flex-1">
          <ViewerStage key={current.id} attachment={current} />
          {phone ? null : (
            <>
              <Step side="left" label={t('viewer.previous')} to={previous} onGo={setCurrent} className="absolute top-1/2 left-6 -translate-y-1/2" />
              <Step side="right" label={t('viewer.next')} to={next} onGo={setCurrent} className="absolute top-1/2 right-6 -translate-y-1/2" />
            </>
          )}
        </div>
        {phone && (previous || next) ? (
          <footer className="flex h-15 flex-none items-center justify-between border-t border-viewer-line px-3">
            <Step side="left" label={t('viewer.previous')} to={previous} onGo={setCurrent} />
            {found && total > 0 ? (
              <span className="font-mono text-[0.78125rem] text-viewer-muted tabular-nums">
                {position} / {total}
              </span>
            ) : null}
            <Step side="right" label={t('viewer.next')} to={next} onGo={setCurrent} />
          </footer>
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

// Step is an arrow to the attachment before or after: over the stage's
// side, or in the phone's bar, where one with nowhere to go keeps its room
// so the others stay put.
function Step({
  side,
  label,
  to,
  onGo,
  className,
}: {
  side: 'left' | 'right'
  label: string
  to?: Attachment
  onGo: (a: Attachment) => void
  className?: string
}) {
  if (!to) return className ? null : <span aria-hidden="true" className="size-11" />
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          onClick={() => onGo(to)}
          aria-label={label}
          className={cn('size-11 rounded-full bg-viewer-control text-viewer-foreground hover:bg-viewer-hover hover:text-viewer-foreground', className)}
        >
          {side === 'left' ? <ChevronLeftIcon className="size-5" /> : <ChevronRightIcon className="size-5" />}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}
