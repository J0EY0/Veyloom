import { useId, useMemo, useState } from 'react'
import { CheckCheckIcon, InboxIcon, SearchXIcon } from 'lucide-react'
import { Link } from 'react-router'
import { PanelHeader } from '@/components/layout/PanelHeader'
import { AgentAvatar } from '@/components/shared/agent-avatar'
import { Chip, ChipText } from '@/components/shared/chip'
import { StatusPill } from '@/components/shared/status-pill'
import { UserAvatar } from '@/components/shared/user-avatar'
import { Button } from '@/components/ui/button'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Label } from '@/components/ui/label'
import { Sidebar, SidebarContent, SidebarGroup, SidebarGroupContent, SidebarHeader, SidebarInput } from '@/components/ui/sidebar'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { useAgentLooks } from '@/lib/agentLooks'
import { formatTime } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { filterEntries, type InboxEntry } from './entries'

export interface InboxListProps {
  entries: InboxEntry[]
  // How many mentions, of all of them, are unread; and marking all read.
  unread: number
  onReadAll?: () => void
  selectedId: string
  loading: boolean
  // Set when the list could not be loaded.
  error?: string
  // Older mentions are a page away.
  hasMore: boolean
  loadingMore: boolean
  onLoadMore: () => void
  className?: string
}

// The list column of the inbox page, built as shadcn's sidebar-09 builds
// its inbox: a title with a switch, a search box, then one row per entry
// (who, when, where, what) with a rule between rows, a mention not read
// lifted onto the page's background. The switch narrows the list to the
// requests waiting for a decision; while any mention is unread, a button
// reads them all.
export function InboxList({ entries, unread, onReadAll, selectedId, loading, error, hasMore, loadingMore, onLoadMore, className }: InboxListProps) {
  const t = useT()
  const switchId = useId()
  const [approvalsOnly, setApprovalsOnly] = useState(false)
  const [query, setQuery] = useState('')
  const shown = useMemo(() => filterEntries(entries, query, approvalsOnly), [entries, query, approvalsOnly])
  const filtering = approvalsOnly || query.trim() !== ''
  const more =
    hasMore && !loading && !approvalsOnly ? (
      <div className="border-t p-3">
        <Button variant="ghost" size="sm" className="w-full text-muted-foreground" onClick={onLoadMore} disabled={loadingMore}>
          {loadingMore ? t('common.loading') : t('inbox.older')}
        </Button>
      </div>
    ) : null

  return (
    <Sidebar collapsible="none" aria-label={t('inbox.title')} className={cn('w-full md:w-85 md:flex-none md:border-r', className)}>
      <SidebarHeader className="gap-0 border-b p-0 pb-3">
        <PanelHeader
          title={t('inbox.title')}
          trailing={
            <div className="flex items-center gap-4">
              {/* In words, not only an icon: the one way to clear the count. */}
              {unread > 0 && onReadAll ? (
                <Button variant="ghost" size="xs" onClick={onReadAll} className="text-[0.8125rem] font-normal text-muted-foreground hover:text-foreground">
                  <CheckCheckIcon data-icon="inline-start" />
                  {t('inbox.readAll')}
                </Button>
              ) : null}
              <div className="flex items-center gap-2">
                <Label htmlFor={switchId} className="text-[0.8125rem] font-normal text-muted-foreground">
                  {t('inbox.approvalsOnly')}
                </Label>
                <Switch id={switchId} checked={approvalsOnly} onCheckedChange={setApprovalsOnly} className="shadow-none" />
              </div>
            </div>
          }
        />
        <div className="px-3">
          <SidebarInput
            type="search"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t('inbox.search')}
            aria-label={t('common.search')}
            className="[&::-webkit-search-cancel-button]:appearance-none"
          />
        </div>
      </SidebarHeader>
      <SidebarContent>
        {/* A note instead of rows sits in the middle of the column. */}
        {!loading && (error !== undefined || shown.length === 0) ? (
          <>
            {error !== undefined ? (
              <Empty className="px-4">
                <EmptyHeader>
                  <EmptyTitle>{t('inbox.failed')}</EmptyTitle>
                  <EmptyDescription>{error}</EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <Empty className="px-4">
                <EmptyHeader>
                  <EmptyMedia variant="icon">{filtering && entries.length > 0 ? <SearchXIcon /> : <InboxIcon />}</EmptyMedia>
                  <EmptyTitle>{entries.length === 0 ? t('inbox.empty') : query.trim() !== '' ? t('inbox.noMatch') : t('inbox.noApprovals')}</EmptyTitle>
                </EmptyHeader>
              </Empty>
            )}
            {more}
          </>
        ) : (
          <SidebarGroup className="p-0">
            <SidebarGroupContent>
              {loading ? (
                <ListSkeleton />
              ) : (
                <ul>
                  {shown.map((entry) => (
                    <li key={entry.id} className="border-b last:border-b-0">
                      <EntryRow entry={entry} active={entry.id === selectedId} />
                    </li>
                  ))}
                </ul>
              )}
              {more}
            </SidebarGroupContent>
          </SidebarGroup>
        )}
      </SidebarContent>
    </Sidebar>
  )
}

function EntryRow({ entry, active }: { entry: InboxEntry; active: boolean }) {
  const t = useT()
  return (
    <Link
      to={{ search: `?item=${entry.id}` }}
      aria-current={active ? 'true' : undefined}
      data-active={active || undefined}
      data-unread={entry.unread || undefined}
      className={cn(
        'flex flex-col items-start gap-2 p-4 text-sm leading-tight outline-hidden transition-colors hover:bg-sidebar-accent/70 focus-visible:bg-sidebar-accent/70 focus-visible:ring-2 focus-visible:ring-sidebar-ring focus-visible:ring-inset data-active:bg-sidebar-accent',
        // In the dark the usual greys sit where an unread row now does, so
        // there they step up: read, unread, pointed at, open, each lighter.
        'dark:hover:bg-accent dark:focus-visible:bg-accent dark:data-active:bg-secondary',
        // Unread rows are lifted off the list's grey onto the page's own
        // background (in the dark, the floating layer's); read ones stay on
        // the grey. The open row is drawn as open.
        entry.unread && !active && 'bg-background dark:bg-popover',
      )}
    >
      {entry.unread ? <span className="sr-only">{t('inbox.unread')}</span> : null}
      <div className="flex w-full min-w-0 items-center gap-2">
        <Sender entry={entry} />
        {entry.kind === 'approval' ? <StatusPill tone="wait">{t('inbox.waiting')}</StatusPill> : null}
        <time dateTime={entry.createdAt} className="ml-auto flex-none text-xs text-subtle tabular-nums">
          {formatTime(entry.createdAt)}
        </time>
      </div>
      <span className="max-w-full truncate text-xs text-muted-foreground">{entry.project}</span>
      <span className={cn('line-clamp-2 w-full text-xs leading-normal break-words text-muted-foreground', entry.kind === 'approval' && 'font-mono')}>
        {entry.excerpt}
      </span>
    </Link>
  )
}

// Sender is who the row is from as a tag, as the skills' facts list
// agents: a member with its agent's face (the runtime's colour, or the
// picture uploaded), a person with the initial, Veyloom by name alone.
// Bold while unread.
function Sender({ entry }: { entry: InboxEntry }) {
  const t = useT()
  const lookOf = useAgentLooks()
  const look = lookOf(entry.agentId)
  const name = entry.sender || t('common.unknown')
  return (
    <Chip className={cn(entry.unread ? 'font-semibold' : 'font-medium', entry.senderKind === 'system' && 'pl-2')}>
      {look ? <AgentAvatar look={look} name={name} size="xs" mark={false} /> : entry.senderKind === 'system' ? null : <UserAvatar name={name} size="xs" />}
      <ChipText>{name}</ChipText>
    </Chip>
  )
}

// Rows in the shape of the real ones while the first page loads.
function ListSkeleton() {
  const t = useT()
  return (
    <div role="status" aria-label={t('common.loading')}>
      {[0, 1, 2].map((row) => (
        <div key={row} className="flex flex-col gap-2.5 border-b p-4 last:border-b-0">
          <div className="flex items-center gap-2">
            <Skeleton className="h-3.5 w-24" />
            <Skeleton className="ml-auto h-3 w-10" />
          </div>
          <Skeleton className="h-3 w-16" />
          <Skeleton className="h-3 w-full" />
        </div>
      ))}
    </div>
  )
}
