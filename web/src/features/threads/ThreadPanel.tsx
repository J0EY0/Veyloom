import { useState, type ReactNode } from 'react'
import { Maximize2Icon, Minimize2Icon } from 'lucide-react'
import { useMessage, useThread, useThreadMessages } from '@/api/messages'
import { useProject } from '@/api/projects'
import { useRoom } from '@/api/rooms'
import { SidePanel } from '@/components/layout/SidePanel'
import { Button } from '@/components/ui/button'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { Composer } from '@/features/rooms/Composer'
import { useMentionTargets } from '@/features/rooms/useMentionTargets'
import { useSenderNames } from '@/features/rooms/useSenderNames'
import { useReadTopic } from '@/features/inbox/useReadTopic'
import { TrustBand, trustedTurns } from '@/features/approvals/TrustBand'
import { MemberLooks } from '@/lib/agentLooks'
import { ThreadTimeline } from './ThreadTimeline'
import { ThreadMeta } from './ThreadMeta'
import { askTitle, ThreadTitle } from './ThreadTitle'
import { useT } from '@/lib/i18n'
import { errorText } from '@/api/errorText'

export interface ThreadPanelProps {
  roomId: string
  roomName: string
  threadId: string
  // Fills the middle column instead of floating over its right side.
  wide?: boolean
  onToggleWide?: () => void
  onClose: () => void
  onOpenTurn?: (turnId: string) => void
  // 'pane' fills a column of its own page rather than floating over the room.
  variant?: 'floating' | 'pane'
  // More header buttons, before the size toggle and the close button.
  actions?: ReactNode
  // Opens another topic: the one a piece of work began in.
  onOpenThread?: (threadId: string) => void
}

// One request and the agents' work on it: the card that floats over the
// room's right side (docs/webui.md §4.2). The message the topic hangs from
// in the chat names it in the header; below, what was said reads as a chat,
// whoever took part.
export function ThreadPanel({ roomId, roomName, threadId, wide, onToggleWide, onClose, onOpenTurn, variant, actions, onOpenThread }: ThreadPanelProps) {
  const thread = useThread(threadId)
  const replies = useThreadMessages(threadId)
  const sender = useSenderNames(roomId)
  const { names, looks } = useMentionTargets(roomId)
  const triggerId = thread.data?.turns[0]?.trigger_message_id ?? ''
  const trigger = useMessage(triggerId)
  // The piece of work the topic's latest turn is part of, and what the
  // person who began it asked (docs/design.md 5.22).
  const work = thread.data?.work
  const chain = useMessage(work?.chain ?? '')
  const asked = [chain.data, trigger.data].find((message) => message?.sender_kind === 'user')
  const [showRoot, setShowRoot] = useState(false)
  const t = useT()
  // The project's wiki and setup topics are called by those names, not by
  // their roots' words.
  const room = useRoom(roomId)
  const project = useProject(room.data?.project_id ?? '')
  const systemTopic = project?.wiki_thread_id === threadId ? 'wiki' : project?.setup_thread_id === threadId ? 'setup' : undefined
  useReadTopic(threadId, [thread.data?.root, ...(replies.data ?? [])])

  const wideLabel = wide ? t('thread.collapse') : t('thread.expand')
  const wideButton = onToggleWide ? (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label={wideLabel} aria-pressed={wide} onClick={onToggleWide} className="text-subtle">
          {wide ? <Minimize2Icon /> : <Maximize2Icon />}
        </Button>
      </TooltipTrigger>
      <TooltipContent side="bottom">{wideLabel}</TooltipContent>
    </Tooltip>
  ) : null
  const pane = variant === 'pane'

  if (thread.isPending) {
    return (
      <SidePanel
        label={t('thread.label')}
        header={<Skeleton className="h-4 w-32" />}
        onClose={onClose}
        closeLabel={t('thread.close')}
        wide={wide}
        variant={variant}
      >
        <div className="flex flex-col gap-3 pt-2">
          <Skeleton className="h-16 w-full rounded-[10px]" />
          <Skeleton className="h-4 w-2/3" />
        </div>
      </SidePanel>
    )
  }
  if (thread.isError) {
    return (
      <SidePanel
        label={t('thread.label')}
        header={<h2 className="text-sm font-semibold">{t('thread.label')}</h2>}
        onClose={onClose}
        closeLabel={t('thread.close')}
        wide={wide}
        variant={variant}
      >
        <Empty>
          <EmptyHeader>
            <EmptyTitle>{t('thread.failed')}</EmptyTitle>
            <EmptyDescription>{errorText(thread.error)}</EmptyDescription>
          </EmptyHeader>
        </Empty>
      </SidePanel>
    )
  }

  const { root, turns } = thread.data
  const ask = asked ? askTitle(asked, names) : undefined
  // A topic a member was woken into begins with the task handed to it,
  // the @ that handed it first.
  const handedBy = root.body !== '' ? root : trigger.data
  const handed = handedBy && /^\s*@/.test(handedBy.body) ? askTitle(handedBy, names) : undefined
  // Without an @, the agent that spoke last answers, counting the root, as
  // the hub routes it (docs/design.md §4.2).
  const lastAgent = [root, ...(replies.data ?? [])].reverse().find((message) => message.sender_kind === 'agent')
  const responder = lastAgent ? sender(lastAgent).name : undefined
  // While a person lets the rest of a turn's requests through, a strip
  // says so and takes it back (docs/design.md 4.6).
  const trusted = trustedTurns(turns)
  const band =
    trusted.length > 0
      ? trusted.map((turn) => {
          const name = trusted.length > 1 ? names.get(turn.member_id) : undefined
          return <TrustBand key={turn.id} turn={turn} name={name} />
        })
      : undefined
  const composer = (
    <Composer roomId={roomId} roomName={roomName} threadId={threadId} hint={responder ? t('composer.replyHint', { name: responder }) : undefined} compact />
  )
  return (
    <SidePanel
      label={t('thread.label')}
      wide={wide}
      variant={variant}
      header={
        systemTopic ? (
          <h2 className="min-w-0 truncate text-sm font-semibold">
            <span className="mr-1 font-normal text-subtle">#{thread.data.thread.number}</span>
            {t(`${systemTopic}Topic.title`)}
          </h2>
        ) : (
          <ThreadTitle
            number={thread.data.thread.number}
            root={root}
            fallback={trigger.data}
            // The topic a person's ask began goes by the ask; one a member
            // was woken into, by the task it was handed, less the @.
            title={ask && (work === undefined || work.thread_id === threadId) ? ask : handed}
            open={showRoot}
            onToggle={() => setShowRoot((open) => !open)}
          />
        )
      }
      subheader={systemTopic ? undefined : <ThreadMeta threadId={threadId} work={work} names={names} ask={ask} onOpenThread={onOpenThread} />}
      band={band}
      headerActions={
        <>
          {actions}
          {wideButton}
        </>
      }
      onClose={onClose}
      closeLabel={t('thread.close')}
      footer={pane ? <div className="mx-auto w-full max-w-186 flex-none">{composer}</div> : composer}
    >
      <MemberLooks.Provider value={looks}>
        <div className={wide || pane ? 'mx-auto max-w-180' : undefined}>
          <ThreadTimeline
            root={root}
            replies={replies.data ?? []}
            turns={turns}
            sender={sender}
            names={names}
            onOpenTurn={onOpenTurn}
            target={threadId}
            withRoot={showRoot}
          />
        </div>
      </MemberLooks.Provider>
    </SidePanel>
  )
}
