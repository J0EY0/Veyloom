import { useCallback, useMemo, useState } from 'react'
import { InboxIcon } from 'lucide-react'
import { useSearchParams } from 'react-router'
import { usePendingApprovalsAll } from '@/api/approvals'
import { useInbox } from '@/api/inbox'
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
// then everything that mentioned you, newest first. A list on the left and
// the picked entry's topic on the right, the way a mail client reads; on a
// phone one of the two at a time. No read state yet (docs/webui.md §8).
export function InboxPage() {
  const t = useT()
  useDocumentTitle(t('inbox.title'))
  const user = useCurrentUser()
  const approvals = usePendingApprovalsAll()
  const inbox = useInbox(user?.id ?? '')
  const entries = useMemo(() => toEntries(approvals.data ?? [], inbox.data?.pages.flat() ?? [], user?.name ?? ''), [approvals.data, inbox.data, user?.name])

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
