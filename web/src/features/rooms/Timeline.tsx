import { useMemo, useState } from 'react'
import { ArrowDownIcon, MessageSquareDashedIcon } from 'lucide-react'
import { useStickToBottomContext } from 'use-stick-to-bottom'
import { usePendingApprovals } from '@/api/approvals'
import type { Approval, RoomMessage } from '@/api/types'
import { useRoomMessages } from '@/api/messages'
import { Conversation, ConversationContent, ConversationEmptyState } from '@/components/ai-elements/conversation'
import { Button } from '@/components/ui/button'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Spinner } from '@/components/ui/spinner'
import { useTopSentinel } from '@/lib/useTopSentinel'
import { cn } from '@/lib/utils'
import { MessageRow } from './MessageRow'
import { useMentionTargets } from './useMentionTargets'
import { useSenderNames } from './useSenderNames'
import { useT } from '@/lib/i18n'
import { MemberLooks } from '@/lib/agentLooks'
import { errorText } from '@/api/errorText'

export interface TimelineProps {
  roomId: string
  onOpenThread?: (threadId: string) => void
  // The project's wiki and setup topics, drawn by name rather than by
  // their roots' words.
  wikiThreadId?: string
  setupThreadId?: string
  // The project, and its note offering a wiki maintainer, drawn as a card.
  projectId?: string
  offerMessageId?: string
  // The topic open beside the chat, whose root is lifted a little.
  openThreadId?: string
  // The project's leader, whose messages say so.
  leaderId?: string
}

// How long after a message the same sender's next one still goes under its
// face.
const GROUP_MS = 5 * 60_000

// continues says the message follows on from the one before it: said by
// the same person or member a moment later, neither a note of the system's.
export function continues(previous: RoomMessage | undefined, message: RoomMessage): boolean {
  if (!previous || previous.sender_kind !== message.sender_kind || message.sender_kind === 'system') return false
  const same = message.sender_kind === 'agent' ? previous.member_id === message.member_id : previous.user_id === message.user_id
  return same && Date.parse(message.created_at) - Date.parse(previous.created_at) < GROUP_MS
}

// The room's top-level messages, newest at the bottom, older pages loading
// as the reader scrolls up. AI Elements' conversation keeps the view
// pinned to the bottom while the reader is there, and offers the way back
// once they have scrolled up.
export function Timeline({ roomId, onOpenThread, wikiThreadId, setupThreadId, projectId, offerMessageId, openThreadId, leaderId }: TimelineProps) {
  const messages = useRoomMessages(roomId)
  const t = useT()
  const sender = useSenderNames(roomId)
  const { names, looks } = useMentionTargets(roomId)
  const pending = usePendingApprovals(roomId)
  const waitingByThread = useMemo(() => {
    const map = new Map<string, Approval>()
    for (const approval of pending.data ?? []) {
      if (!map.has(approval.thread_id)) map.set(approval.thread_id, approval)
    }
    return map
  }, [pending.data])
  const list = useMemo(() => messages.data?.pages.flat() ?? [], [messages.data])
  const nothing = messages.isError || (messages.isSuccess && list.length === 0)

  const topSentinel = useTopSentinel(() => {
    if (messages.hasPreviousPage && !messages.isFetchingPreviousPage) {
      void messages.fetchPreviousPage()
    }
  })

  return (
    <Conversation className="min-h-0 flex-1" initial="instant" resize="smooth">
      {/* With nothing to show, the content is as tall as the view, so the
          note takes the rest of it and sits in the middle. */}
      <ConversationContent className={cn('gap-0 p-0 pt-3 pb-2', nothing && 'min-h-full')}>
        {messages.isPending ? (
          <p role="status" className="flex items-center gap-2 px-6 py-3 text-sm text-subtle">
            <Spinner className="size-3.5" />
            {t('common.loading')}
          </p>
        ) : messages.isError ? (
          <Empty>
            <EmptyHeader>
              <EmptyTitle>{t('timeline.failed')}</EmptyTitle>
              <EmptyDescription>{errorText(messages.error)}</EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : list.length === 0 ? (
          <ConversationEmptyState
            className="flex-1"
            icon={<MessageSquareDashedIcon className="size-5" />}
            title={t('timeline.empty')}
            // Empty states are one line (docs/webui.md); this drops the
            // English default description.
            description=""
          />
        ) : (
          <>
            <div ref={topSentinel} aria-hidden="true" className="h-px" />
            {messages.isFetchingPreviousPage ? (
              <p role="status" className="flex items-center justify-center gap-2 py-2 text-xs text-subtle">
                <Spinner className="size-3" />
                {t('timeline.loadingOlder')}
              </p>
            ) : null}
            <MemberLooks.Provider value={looks}>
              <ol>
                {list.map((message, index) => (
                  <MessageRow
                    key={message.id}
                    message={message}
                    sender={sender(message)}
                    names={names}
                    continued={continues(list[index - 1], message)}
                    selected={message.thread !== undefined && message.thread.id === openThreadId}
                    leader={leaderId !== undefined && message.member_id === leaderId}
                    worker={workerOf(message, names)}
                    approval={message.thread ? waitingByThread.get(message.thread.id) : undefined}
                    systemTopic={
                      message.thread === undefined
                        ? undefined
                        : message.thread.id === wikiThreadId
                          ? 'wiki'
                          : message.thread.id === setupThreadId
                            ? 'setup'
                            : undefined
                    }
                    offerProjectId={offerMessageId !== undefined && message.id === offerMessageId ? projectId : undefined}
                    onOpenThread={onOpenThread}
                  />
                ))}
              </ol>
            </MemberLooks.Provider>
          </>
        )}
      </ConversationContent>
      <ScrollDown lastId={list[list.length - 1]?.id ?? ''} />
    </Conversation>
  )
}

// workerOf names the member whose turns a topic has, when it is not whoever
// said the root: the member a leader's message woke.
function workerOf(message: RoomMessage, names: Map<string, string>): string | undefined {
  const member = message.thread?.last_turn?.member_id
  return member && member !== message.member_id ? names.get(member) : undefined
}

// The button back to the bottom: once the reader has scrolled up, and
// worded as news when something arrived while they were away.
function ScrollDown({ lastId }: { lastId: string }) {
  const { isAtBottom, scrollToBottom } = useStickToBottomContext()
  const t = useT()
  // The last message seen at the bottom; a different one now means news.
  const [seen, setSeen] = useState(lastId)
  if (isAtBottom && seen !== lastId) setSeen(lastId)
  if (isAtBottom) return null
  const unseen = seen !== lastId
  return (
    <Button
      type="button"
      variant="outline"
      size="sm"
      onClick={() => void scrollToBottom()}
      className="absolute bottom-3 left-1/2 -translate-x-1/2 animate-in rounded-full bg-popover shadow-pop duration-200 fade-in-0 slide-in-from-bottom-2"
    >
      <ArrowDownIcon data-icon="inline-start" />
      {unseen ? t('timeline.newMessages') : t('timeline.toBottom')}
    </Button>
  )
}
