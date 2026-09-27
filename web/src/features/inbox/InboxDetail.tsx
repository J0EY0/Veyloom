import { useCallback } from 'react'
import { InboxIcon, MessagesSquareIcon } from 'lucide-react'
import { Link, useSearchParams } from 'react-router'
import { useRoomEvents } from '@/api/events'
import { useTurn } from '@/api/turns'
import { SidePanel } from '@/components/layout/SidePanel'
import { AgentAvatar } from '@/components/shared/agent-avatar'
import { MessageAttachments } from '@/features/attachments/MessageAttachments'
import { UserAvatar } from '@/components/shared/user-avatar'
import { Button } from '@/components/ui/button'
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { AgentBody } from '@/features/rooms/AgentBody'
import { MessageBody } from '@/features/rooms/MessageBody'
import { useMentionTargets } from '@/features/rooms/useMentionTargets'
import { ThreadPanel } from '@/features/threads/ThreadPanel'
import { TurnDrawer } from '@/features/turns/TurnDrawer'
import { MemberLooks } from '@/lib/agentLooks'
import { formatTime } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import type { InboxEntry } from './entries'

export interface InboxDetailProps {
  // Nothing picked yet.
  entry?: InboxEntry
  // The list has nothing to pick from, so there is nothing to hint at.
  listEmpty?: boolean
  onClose: () => void
  className?: string
}

// The reading side of the inbox: the topic the picked entry belongs
// to, live, with its approval cards and a reply box, as the chat would show
// it in its side panel.
export function InboxDetail({ entry, listEmpty, onClose, className }: InboxDetailProps) {
  const t = useT()
  return (
    <div className={cn('min-w-0 flex-1 flex-col', className)}>
      {entry ? (
        <EntryTopic key={entry.id} entry={entry} onClose={onClose} />
      ) : listEmpty ? null : (
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <InboxIcon />
            </EmptyMedia>
            <EmptyTitle>{t('inbox.pick')}</EmptyTitle>
          </EmptyHeader>
        </Empty>
      )}
    </div>
  )
}

function EntryTopic({ entry, onClose }: { entry: InboxEntry; onClose: () => void }) {
  const t = useT()
  // The answer heading a topic names only its turn; the turn names the
  // topic.
  const turn = useTurn(entry.threadId ? '' : (entry.turnId ?? ''))
  const threadId = entry.threadId ?? turn.data?.thread_id ?? ''
  useRoomEvents(entry.roomId)
  const [params, setParams] = useSearchParams()
  const drawerTurn = params.get('turn') ?? ''
  const openTurn = useCallback(
    (id: string) => {
      setParams((current) => {
        const next = new URLSearchParams(current)
        if (id) next.set('turn', id)
        else next.delete('turn')
        return next
      })
    },
    [setParams],
  )

  const openInChat = (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button variant="ghost" size="icon-sm" asChild className="text-subtle hover:text-foreground">
          <Link to={`/rooms/${entry.roomId}${threadId ? `?thread=${threadId}` : ''}`} aria-label={t('inbox.openInChat')}>
            <MessagesSquareIcon />
          </Link>
        </Button>
      </TooltipTrigger>
      <TooltipContent side="bottom">{t('inbox.openInChat')}</TooltipContent>
    </Tooltip>
  )

  if (threadId) {
    return (
      <>
        <ThreadPanel
          variant="pane"
          roomId={entry.roomId}
          roomName={entry.project}
          threadId={threadId}
          onClose={onClose}
          onOpenTurn={openTurn}
          actions={openInChat}
        />
        <TurnDrawer roomId={entry.roomId} turnId={drawerTurn} onClose={() => openTurn('')} />
      </>
    )
  }
  if (entry.turnId && turn.isPending) {
    return (
      <SidePanel variant="pane" label={t('thread.label')} header={<Skeleton className="h-4 w-32" />} onClose={onClose} closeLabel={t('thread.close')}>
        <div className="mx-auto flex max-w-180 flex-col gap-3 pt-2">
          <Skeleton className="h-16 w-full rounded-[10px]" />
          <Skeleton className="h-4 w-2/3" />
        </div>
      </SidePanel>
    )
  }
  return <MessageAlone entry={entry} onClose={onClose} actions={openInChat} />
}

// A mention that belongs to no topic, a person's message in the chat, is
// read on its own.
function MessageAlone({ entry, onClose, actions }: { entry: InboxEntry; onClose: () => void; actions: React.ReactNode }) {
  const t = useT()
  const { names, looks } = useMentionTargets(entry.roomId)
  const message = entry.message
  const look = message?.sender_kind === 'agent' ? looks.get(message.member_id ?? '') : undefined
  return (
    <SidePanel
      variant="pane"
      label={t('room.title')}
      header={
        <>
          {look ? <AgentAvatar look={look} name={entry.sender || '?'} size="message" /> : <UserAvatar name={entry.sender || '?'} size="message" />}
          <h2 className="truncate text-sm font-semibold">{entry.sender || t('common.unknown')}</h2>
          <time dateTime={entry.createdAt} className="flex-none text-xs text-subtle">
            {formatTime(entry.createdAt)}
          </time>
        </>
      }
      headerActions={actions}
      onClose={onClose}
      closeLabel={t('common.close')}
    >
      {message ? (
        <MemberLooks.Provider value={looks}>
          <div className="mx-auto max-w-180 pt-1">
            {message.sender_kind === 'agent' ? (
              <AgentBody message={message} names={names} target="room" />
            ) : (
              <MessageBody
                body={message.body}
                mentions={message.mentions}
                names={names}
                className="text-[0.90625rem] leading-[1.6] break-words whitespace-pre-wrap text-body"
              />
            )}
            <MessageAttachments attachments={message.attachments} roomId={message.room_id} threadId={message.thread_id} className="mt-2" />
          </div>
        </MemberLooks.Provider>
      ) : null}
    </SidePanel>
  )
}
