import { useCallback, useMemo, useState } from 'react'
import { DownloadIcon, PaperclipIcon } from 'lucide-react'
import { useSearchParams } from 'react-router'
import { archiveUrl, attachmentUrl, useRoomAttachments } from '@/api/attachments'
import { errorText } from '@/api/errorText'
import type { RoomAttachment } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { formatBytes, formatCount, formatDay } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { useInView } from '@/lib/useInView'
import { cn } from '@/lib/utils'
import { AttachmentCard } from './AttachmentCard'
import { AttachmentsToolbar } from './AttachmentsToolbar'
import { byDay, queryOf, readFilter, relativeDay, splitDays, whenSent, writeFilter, type TabFilter } from './filter'
import { openViewer } from './viewerStore'

// The most the hub packs into one download.
export const PACK_MAX = 500

// Four cards a row where there is room, fewer on a narrower chat.
const grid = 'grid grid-cols-2 gap-x-5 gap-y-6 @2xl:grid-cols-3 @4xl:grid-cols-4'

// AttachmentsView is the chat's Attachments tab (docs/webui.md 4.21):
// every file the room's messages brought, its topics' too, as cards; the
// newest first in days by default. A card opens the viewer, which walks
// the cards in the same order. Picking some downloads them as one zip.
export function AttachmentsView({ roomId }: { roomId: string }) {
  const t = useT()
  const [params, setParams] = useSearchParams()
  const filter = readFilter(params)
  const { q, group, sender, sort } = filter
  const query = useMemo(() => queryOf({ q, group, sender, sort }), [q, group, sender, sort])
  const list = useRoomAttachments(roomId, query)
  const shown = useMemo(() => list.data?.pages.flatMap((page) => page.attachments) ?? [], [list.data])
  const total = list.data?.pages[0]?.total
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = list
  const more = useInView<HTMLDivElement>(
    () => {
      if (hasNextPage && !isFetchingNextPage) void fetchNextPage()
    },
    { once: false, margin: '600px' },
  )
  const change = useCallback((next: Partial<TabFilter>) => setParams((prev) => writeFilter(prev, next), { replace: true }), [setParams])

  const [picking, setPicking] = useState(false)
  const [picked, setPicked] = useState<ReadonlyMap<string, RoomAttachment>>(new Map())
  const allPicked = shown.length > 0 && shown.every((a) => picked.has(a.id))
  function pick(attachment: RoomAttachment) {
    setPicked((prev) => {
      const next = new Map(prev)
      if (next.has(attachment.id)) next.delete(attachment.id)
      else next.set(attachment.id, attachment)
      return next
    })
  }
  function stopPicking() {
    setPicking(false)
    setPicked(new Map())
  }

  const inDays = byDay(sort)
  const now = new Date()
  const card = (attachment: RoomAttachment) => (
    <AttachmentCard
      key={attachment.id}
      attachment={attachment}
      when={whenSent(attachment.created_at, inDays, now)}
      picking={picking}
      picked={picked.has(attachment.id)}
      onOpen={() => openViewer({ roomId, attachment, threadId: attachment.thread_id, filter: query })}
      onPick={() => pick(attachment)}
    />
  )

  const narrowed = q.trim() !== '' || group !== 'all' || sender !== ''
  // A room with nothing in it has nothing to search, filter or pick: only
  // the one line, in the middle (docs/webui.md §0).
  const bare = list.isSuccess && shown.length === 0 && !narrowed

  let body
  if (list.isPending) {
    body = (
      <div role="status" aria-label={t('common.loading')} className={grid}>
        {Array.from({ length: 8 }, (_, i) => (
          <div key={i} className="flex flex-col gap-2">
            <Skeleton className="aspect-4/3 rounded-xl" />
            <Skeleton className="h-3 w-2/3" />
          </div>
        ))}
      </div>
    )
  } else if (list.isError) {
    body = (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>{t('attachments.failed')}</EmptyTitle>
          <EmptyDescription>{errorText(list.error)}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else if (shown.length === 0) {
    body = (
      <Empty>
        <EmptyHeader>
          {narrowed ? null : (
            <EmptyMedia variant="icon">
              <PaperclipIcon />
            </EmptyMedia>
          )}
          <EmptyTitle>{narrowed ? t('attachments.noMatch') : t('attachments.none')}</EmptyTitle>
        </EmptyHeader>
      </Empty>
    )
  } else if (inDays) {
    const days = splitDays(shown)
    body = (
      <div className="flex flex-col gap-9">
        {days.map((day, i) => (
          <section key={day.key} aria-label={relativeDay(day.at) ?? formatDay(day.at)}>
            {/* A day the next page may go on with is not counted yet. */}
            <DayHeading at={day.at} count={i < days.length - 1 || !hasNextPage ? day.attachments.length : undefined} />
            <div className={grid}>{day.attachments.map(card)}</div>
          </section>
        ))}
      </div>
    )
  } else {
    body = <div className={grid}>{shown.map(card)}</div>
  }

  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      {bare ? null : (
        <div className="flex-none px-5 pt-4.5 pb-2.5">
          <div className="mx-auto max-w-250">
            <AttachmentsToolbar
              roomId={roomId}
              filter={filter}
              onChange={change}
              // Days count their own; a list not in days is counted up here.
              total={inDays ? undefined : total}
              picking={picking}
              onPicking={(on) => (on ? setPicking(true) : stopPicking())}
              allPicked={allPicked}
              onPickAll={(all) => setPicked(all ? new Map(shown.map((a) => [a.id, a])) : new Map())}
            />
          </div>
        </div>
      )}
      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
        <div className={cn('@container mx-auto flex w-full max-w-250 flex-1 flex-col px-5', bare ? 'py-5' : 'pt-3 pb-24')}>
          {body}
          {/* A new one after each page, so a short page still asks for the next. */}
          <div key={shown.length} ref={more} aria-hidden="true" className="h-px" />
          {isFetchingNextPage ? (
            <p role="status" className="flex items-center justify-center gap-2 py-4 text-xs text-subtle">
              <Spinner className="size-3" />
              {t('attachments.loadingMore')}
            </p>
          ) : null}
        </div>
      </div>
      {picked.size > 0 ? <PackBar roomId={roomId} picked={picked} onCancel={stopPicking} /> : null}
    </div>
  )
}

function DayHeading({ at, count }: { at: string; count?: number }) {
  const t = useT()
  const relative = relativeDay(at)
  return (
    <div className="mb-3 flex items-baseline gap-2.5">
      <h2 className="text-sm font-semibold text-foreground">{relative ?? formatDay(at)}</h2>
      {relative ? <span className="text-[0.78125rem] text-subtle tabular-nums">{formatDay(at)}</span> : null}
      <span className="grow" />
      {count !== undefined ? <span className="text-xs text-subtle tabular-nums">{t('attachments.count', { n: formatCount(count) })}</span> : null}
    </div>
  )
}

// PackBar floats under the cards while some are picked: how many and how
// big, and one download of them all (only one: that file itself).
function PackBar({ roomId, picked, onCancel }: { roomId: string; picked: ReadonlyMap<string, RoomAttachment>; onCancel: () => void }) {
  const t = useT()
  const files = [...picked.values()]
  const size = files.reduce((sum, a) => sum + a.size, 0)
  const one = files.length === 1 ? files[0] : undefined
  const label = one ? t('attachment.downloadShort') : t('attachments.pack')
  return (
    <div className="absolute bottom-5 left-1/2 flex max-w-[calc(100%-2rem)] -translate-x-1/2 items-center gap-2.5 rounded-xl border bg-popover py-2 pr-2 pl-4 shadow-elev">
      <span role="status" className="truncate text-[0.8125rem] whitespace-nowrap tabular-nums">
        {t('attachments.picked', { n: formatCount(files.length), size: formatBytes(size) })}
      </span>
      <Button variant="outline" size="sm" onClick={onCancel}>
        {t('common.cancel')}
      </Button>
      {files.length > PACK_MAX ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <span tabIndex={0}>
              <Button size="sm" disabled>
                <DownloadIcon />
                {label}
              </Button>
            </span>
          </TooltipTrigger>
          <TooltipContent>{t('attachments.tooMany', { n: PACK_MAX })}</TooltipContent>
        </Tooltip>
      ) : (
        <Button asChild size="sm">
          <a
            href={
              one
                ? attachmentUrl(one.id)
                : archiveUrl(
                    roomId,
                    files.map((a) => a.id),
                  )
            }
            download={one ? one.filename : ''}
          >
            <DownloadIcon />
            {label}
          </a>
        </Button>
      )}
    </div>
  )
}
