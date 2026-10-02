import { useCallback, useEffect, useMemo, useState } from 'react'
import { ViewTransition } from 'react'
import { SearchIcon, UsersIcon } from 'lucide-react'
import { useMatch, useParams, useSearchParams } from 'react-router'
import { ApiError } from '@/api/client'
import { usePrefetchAddressee } from '@/api/addressee'
import { usePendingApprovals } from '@/api/approvals'
import { useRoomEvents } from '@/api/events'
import { useProject } from '@/api/projects'
import { useRoom } from '@/api/rooms'
import type { WikiSpace } from '@/api/wiki'
import { Panel } from '@/components/layout/Panel'
import { PanelHeader } from '@/components/layout/PanelHeader'
import { Button } from '@/components/ui/button'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { MembersPanel } from '@/features/members/MembersPanel'
import { ApprovalsPanel } from '@/features/approvals/ApprovalsPanel'
import { ThreadPanel } from '@/features/threads/ThreadPanel'
import { WikiView } from '@/features/wiki/WikiView'
import { TasksView } from '@/features/tasks/TasksView'
import { AttachmentsView } from '@/features/attachments/AttachmentsView'
import { TurnDrawer } from '@/features/turns/TurnDrawer'
import { useDocumentTitle } from '@/lib/useDocumentTitle'
import { useEscape } from '@/lib/useEscape'
import { rememberChat } from '@/lib/lastChat'
import { openPalette } from '@/lib/palette'
import { useOutsidePress } from '@/lib/useOutsidePress'
import { MemberIsland } from './MemberIsland'
import {
  FACES_REM,
  ISLAND_COUNT_REM,
  ISLAND_ROW_REM,
  ISLAND_WORDY_REM,
  NARROW_PANEL_REM,
  PANEL_REM,
  VIEWS_ROW_REM,
  panelLayout,
  useWidthRem,
} from './panelLayout'
import { Composer } from './Composer'
import { ConnectionHint } from './ConnectionHint'
import { RoomMenu } from './RoomMenu'
import { RoomTabs } from './RoomTabs'
import { Timeline } from './Timeline'
import { islandSays, useMemberStates } from './useMemberStates'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { errorText } from '@/api/errorText'

export function RoomPage() {
  const { roomId = '' } = useParams()
  const t = useT()
  const room = useRoom(roomId)
  // The chat's box says whom a message without an @ goes to.
  usePrefetchAddressee(roomId)
  useRoomEvents(roomId)
  const memberStates = useMemberStates(roomId)
  const pending = usePendingApprovals(roomId)
  // The Wiki tab is this page too: its part of the address picks the page.
  const wikiMatch = useMatch('/rooms/:roomId/wiki/*')
  const tasksMatch = useMatch('/rooms/:roomId/tasks/*')
  const attachmentsMatch = useMatch('/rooms/:roomId/attachments')
  const projectId = room.data?.project_id ?? ''
  const wikiSpace = useMemo<WikiSpace>(() => ({ kind: 'project', projectId, roomId }), [projectId, roomId])
  const pendingCount = pending.data?.length ?? 0
  // A project is its chat: the chat is titled after the project.
  const project = useProject(projectId)
  const projectName = project?.name
  const chatName = projectName ?? room.data?.name ?? ''
  useDocumentTitle(room.data ? `${pendingCount > 0 ? `(${pendingCount}) ` : ''}${chatName}` : '')
  // Opening Veyloom comes back here (see HomeRedirect).
  const found = room.data !== undefined
  useEffect(() => {
    if (found) rememberChat(roomId)
  }, [found, roomId])

  // The open topic lives in the URL, so it survives reloads and can be linked.
  const [params, setParams] = useSearchParams()
  const threadId = params.get('thread') ?? ''
  const panel = params.get('panel') ?? ''
  const turnId = params.get('turn') ?? ''
  const [wide, setWide] = useState(false)
  const openThread = useCallback(
    (id: string) => {
      setParams((current) => {
        const next = new URLSearchParams(current)
        next.set('thread', id)
        next.delete('panel')
        return next
      })
    },
    [setParams],
  )
  const openPanel = useCallback(
    (name: string) => {
      setParams((current) => {
        const next = new URLSearchParams(current)
        next.set('panel', name)
        next.delete('thread')
        return next
      })
    },
    [setParams],
  )
  const closePanel = useCallback(() => {
    setParams((current) => {
      const next = new URLSearchParams(current)
      next.delete('panel')
      return next
    })
  }, [setParams])
  const closeThread = useCallback(() => {
    setParams((current) => {
      const next = new URLSearchParams(current)
      next.delete('thread')
      next.delete('turn')
      return next
    })
  }, [setParams])
  const openTurn = useCallback(
    (id: string) => {
      setParams((current) => {
        const next = new URLSearchParams(current)
        next.set('turn', id)
        return next
      })
    },
    [setParams],
  )
  const closeTurn = useCallback(() => {
    setParams((current) => {
      const next = new URLSearchParams(current)
      next.delete('turn')
      return next
    })
  }, [setParams])
  const sideOpen = threadId !== '' || panel !== ''
  // Escape peels the layers back: drawer, then panel, then topic.
  const escape = useCallback(() => {
    if (turnId) closeTurn()
    else if (panel) closePanel()
    else if (threadId) closeThread()
  }, [turnId, panel, threadId, closeTurn, closePanel, closeThread])
  useEscape(escape, sideOpen || turnId !== '')

  // Beside a wide enough chat the panel pushes it aside; otherwise it lies
  // over the chat and a press anywhere else closes it (see panelLayout).
  // A topic made wide covers the chat by choice.
  const [bodyRef, bodyRem] = useWidthRem()
  // The chat's info is narrow, unless it lists branches.
  const layout = panelLayout(bodyRem, panel === 'members' && !project?.repo_path ? NARROW_PANEL_REM : PANEL_REM)
  // The top bar is as wide as the chat (see panelLayout): a narrow one, a
  // tablet's beside the sidebar, gives up the idle count, then what the
  // island says takes a row of its own under the bar, as wide as the chat
  // (and the bar gets back the members button), then the views fold into a
  // menu; a phone's keeps out the faces and the members button too.
  const narrowBar = (rem: number) => bodyRem > 0 && bodyRem < rem
  const islandBelow = narrowBar(ISLAND_ROW_REM) && islandSays(memberStates)
  const viewsFolded = narrowBar(VIEWS_ROW_REM)
  const phoneBar = narrowBar(FACES_REM)
  const islandInBar = memberStates.length > 0 && !islandBelow && !phoneBar
  const terse = islandBelow || narrowBar(ISLAND_WORDY_REM)
  const beside = sideOpen && !wide
  const closeSide = useCallback(() => {
    if (panel) closePanel()
    else if (threadId) closeThread()
  }, [panel, threadId, closePanel, closeThread])
  const pressedInside = useOutsidePress(closeSide, { ignore: '[data-opens-panel]', enabled: beside && layout.mode === 'overlay' })
  const padRem = beside && layout.mode === 'push' ? layout.padRem : 0

  if (room.isPending) {
    return (
      <Panel>
        <PanelHeader title={<span className="text-subtle">{t('common.loading')}</span>} />
      </Panel>
    )
  }
  if (room.isError) {
    const missing = room.error instanceof ApiError && room.error.status === 404
    return (
      <Panel>
        <PanelHeader title={t('room.title')} />
        <Empty>
          <EmptyHeader>
            <EmptyTitle>{missing ? t('room.missing') : t('room.failed')}</EmptyTitle>
            <EmptyDescription>{errorText(room.error)}</EmptyDescription>
          </EmptyHeader>
        </Empty>
      </Panel>
    )
  }
  return (
    <Panel>
      <PanelHeader
        title={chatName}
        actions={
          <>
            <RoomMenu roomId={roomId} />
            <RoomTabs roomId={roomId} view={wikiMatch ? 'wiki' : tasksMatch ? 'tasks' : attachmentsMatch ? 'attachments' : 'chat'} folded={viewsFolded} />
          </>
        }
        trailing={
          <>
            {islandInBar ? (
              <MemberIsland
                states={memberStates}
                onOpenThread={openThread}
                onOpenMembers={() => openPanel('members')}
                facesOnly={narrowBar(ISLAND_COUNT_REM)}
                terse={terse}
              />
            ) : null}
            <ConnectionHint />
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={t('room.info')}
                  aria-pressed={panel === 'members'}
                  data-opens-panel
                  onClick={() => (panel === 'members' ? closePanel() : openPanel('members'))}
                  // In a narrow bar, beside the island there is no room for it,
                  // nor in a phone's at all; the island and the chat's menu open
                  // the same.
                  className={cn('text-subtle hover:text-foreground', (phoneBar || (islandInBar && viewsFolded)) && 'hidden')}
                >
                  <UsersIcon />
                </Button>
              </TooltipTrigger>
              <TooltipContent side="bottom">{t('room.info')}</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button variant="ghost" size="icon-sm" aria-label={t('common.search')} onClick={openPalette} className="text-subtle hover:text-foreground">
                  <SearchIcon />
                </Button>
              </TooltipTrigger>
              <TooltipContent side="bottom">{t('common.search')}</TooltipContent>
            </Tooltip>
          </>
        }
      />
      {islandBelow ? (
        <div className="flex-none px-3 pb-2">
          <MemberIsland states={memberStates} onOpenThread={openThread} onOpenMembers={() => openPanel('members')} terse className="w-full max-w-none" />
        </div>
      ) : null}
      {/* Under the top bar, which never moves: the chat, and on its right the open panel. */}
      <div ref={bodyRef} className="relative flex min-h-0 flex-1 flex-col">
        <div
          data-room-chat
          className="flex min-h-0 flex-1 flex-col transition-[padding] duration-200"
          style={padRem > 0 ? { paddingRight: `${padRem}rem` } : undefined}
        >
          {wikiMatch ? (
            <WikiView space={wikiSpace} rest={wikiMatch.params['*'] ?? ''} onOpenThread={openThread} />
          ) : tasksMatch ? (
            <TasksView roomId={roomId} chain={tasksMatch.params['*'] ?? ''} onOpenThread={openThread} />
          ) : attachmentsMatch ? (
            <AttachmentsView roomId={roomId} />
          ) : (
            <>
              <div className="relative flex min-h-0 flex-1 flex-col">
                <Timeline
                  key={roomId}
                  roomId={roomId}
                  onOpenThread={openThread}
                  wikiThreadId={project?.wiki_thread_id}
                  setupThreadId={project?.setup_thread_id}
                  projectId={project?.id}
                  offerMessageId={project?.wiki_offer_message_id}
                  openThreadId={threadId}
                  leaderId={project?.leader_id}
                />
              </div>
              <Composer roomId={roomId} roomName={chatName} />
            </>
          )}
        </div>
        {/* React's tree, not the DOM's: what a panel opens in portals is still inside it. */}
        <div className="contents" onPointerDownCapture={pressedInside}>
          {panel === 'approvals' ? (
            <ViewTransition enter="panel-in" exit="panel-out" default="none">
              <ApprovalsPanel roomId={roomId} onClose={closePanel} onOpenThread={openThread} />
            </ViewTransition>
          ) : panel === 'members' ? (
            <ViewTransition enter="panel-in" exit="panel-out" default="none">
              <MembersPanel roomId={roomId} roomName={chatName} onClose={closePanel} onOpenThread={openThread} />
            </ViewTransition>
          ) : threadId ? (
            <ViewTransition enter="panel-in" exit="panel-out" default="none">
              <ThreadPanel
                key={threadId}
                roomId={roomId}
                roomName={chatName}
                threadId={threadId}
                wide={wide}
                onToggleWide={() => setWide((value) => !value)}
                onClose={closeThread}
                onOpenTurn={openTurn}
                onOpenThread={openThread}
              />
            </ViewTransition>
          ) : null}
        </div>
      </div>
      <TurnDrawer roomId={roomId} turnId={turnId} onClose={closeTurn} />
    </Panel>
  )
}
