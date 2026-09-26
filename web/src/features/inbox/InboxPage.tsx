import { useCallback, useEffect, useMemo, useState } from 'react'
import { InboxIcon } from 'lucide-react'
import { useSearchParams } from 'react-router'
import { usePendingApprovalsAll } from '@/api/approvals'
import { useInbox, useMarkInboxRead } from '@/api/inbox'
import { Panel } from '@/components/layout/Panel'
import { PanelHeader } from '@/components/layout/PanelHeader'
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { useCurrentUser } from '@/lib/currentUser'
import { useDocumentTitle } from '@/lib/useDocumentTitle'
import { useEscape } from '@/lib/useEscape'
import { useT } from '@/lib/i18n'
import { toEntries, type InboxEntry } from './entries'
import { InboxDetail } from './InboxDetail'
import { InboxList } from './InboxList'
import { errorText } from '@/api/errorText'

// What waits for you, across every project: requests to approve first,
// then everything that mentioned you, newest first, what you have not read
// marked. A list on the left and the picked entry's topic on the right,
// the way a mail client reads; on a phone one of the two at a time.
// Opening a mention reads it (docs/webui.md 4.19).
export function InboxPage() {
  const t = useT()
  useDocumentTitle(t('inbox.title'))
  const user = useCurrentUser()
  const approvals = usePendingApprovalsAll()
  const inbox = useInbox(user?.id ?? '')
  const { mutate: markRead } = useMarkInboxRead(user?.id ?? '')
  const entries = useMemo(
    () => toEntries(approvals.data ?? [], inbox.data?.pages.flatMap((page) => page.items) ?? [], user?.name ?? '', t),
    [approvals.data, inbox.data, user?.name, t],
  )
  const unread = inbox.data?.pages[0]?.unread ?? 0
  // Everything listed is read at once, up to the newest mention in sight.
  const newest = entries.find((entry) => entry.kind === 'mention')?.seq

  // The picked entry lives in the URL, so it survives a reload and Back
  // returns to the list.
  const [params, setParams] = useSearchParams()
  const itemId = params.get('item') ?? ''
  // The picked entry as last seen, so its topic stays open after it leaves
  // the list: an approval decided in place stops being pending.
  const [kept, setKept] = useState<InboxEntry>()
  const found = entries.find((entry) => entry.id === itemId)
  if (found && found !== kept) setKept(found)
  const selected = found ?? (kept?.id === itemId ? kept : undefined)
  const close = useCallback(() => {
    setParams((current) => {
      const next = new URLSearchParams(current)
      next.delete('item')
      next.delete('turn')
      return next
    })
  }, [setParams])
  useEscape(close, itemId !== '')
  const opened = found?.kind === 'mention' && found.unread ? found.id : undefined
  useEffect(() => {
    if (opened) markRead({ message_ids: [opened] })
  }, [opened, markRead])

  const loading = inbox.isPending || approvals.isPending
  // Nothing waits, and nothing is open: no list to search or narrow, only
  // the note, in the middle of the page.
  const nothing = user && !loading && !inbox.isError && entries.length === 0 && !selected
  if (!user || nothing) {
    return (
      <Panel>
        <PanelHeader title={t('inbox.title')} />
        <Empty>
          <EmptyHeader>
            {nothing ? (
              <EmptyMedia variant="icon">
                <InboxIcon />
              </EmptyMedia>
            ) : null}
            <EmptyTitle>{nothing ? t('inbox.empty') : t('common.identifying')}</EmptyTitle>
          </EmptyHeader>
        </Empty>
      </Panel>
    )
  }
  return (
    <Panel className="flex-row">
      <InboxList
        entries={entries}
        unread={unread}
        onReadAll={newest !== undefined ? () => markRead({ up_to: newest }) : undefined}
        selectedId={itemId}
        loading={loading}
        error={inbox.isError ? errorText(inbox.error) : undefined}
        hasMore={inbox.hasNextPage}
        loadingMore={inbox.isFetchingNextPage}
        onLoadMore={() => void inbox.fetchNextPage()}
        className={selected ? 'hidden md:flex' : undefined}
      />
      <InboxDetail entry={selected} listEmpty={entries.length === 0} onClose={close} className={selected ? 'flex' : 'hidden md:flex'} />
    </Panel>
  )
}
