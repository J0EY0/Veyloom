import { useLocation } from 'react-router'
import { useRoomTasks } from '@/api/work'
import { HeaderTabs } from '@/components/layout/HeaderTabs'
import { useT } from '@/lib/i18n'
import { wikiHref } from '@/features/wiki/links'

// RoomView is which of the chat's views is open.
export type RoomView = 'chat' | 'tasks' | 'wiki' | 'attachments'

export interface RoomTabsProps {
  roomId: string
  view: RoomView
  // A bar too narrow for the row: the open view and a menu of the four.
  folded?: boolean
}

// The chat's views, in its top bar (docs/design.md 5.14, webui.md 4.20,
// 4.21): the chat, the task board, the project wiki and the attachments.
// The branches are in the chat's info and on the tasks they carry (design.md
// 5.21); the Tasks tab counts the work waiting to be merged, which a folded
// row shows in its menu. Switching keeps the open topic or panel open.
export function RoomTabs({ roomId, view, folded }: RoomTabsProps) {
  const t = useT()
  const { search } = useLocation()
  const tasks = useRoomTasks(roomId)
  const toMerge = tasks.data?.filter((task) => task.state === 'merge').length ?? 0
  return (
    <HeaderTabs
      label={t('room.views')}
      folded={folded}
      tabs={[
        { to: { pathname: `/rooms/${roomId}`, search }, active: view === 'chat', label: t('room.tab.chat') },
        {
          to: { pathname: `/rooms/${roomId}/tasks`, search },
          active: view === 'tasks',
          label: t('room.tab.tasks'),
          badge:
            toMerge > 0 ? (
              <>
                <span
                  aria-hidden="true"
                  className="flex h-4 min-w-4 items-center justify-center rounded-full bg-foreground/10 px-1 text-[0.65625rem] font-semibold text-foreground tabular-nums"
                >
                  {toMerge}
                </span>
                <span className="sr-only"> {t('tasks.toMerge', { n: toMerge })}</span>
              </>
            ) : undefined,
        },
        { to: { pathname: wikiHref(roomId), search }, active: view === 'wiki', label: t('room.tab.wiki') },
        { to: { pathname: `/rooms/${roomId}/attachments`, search }, active: view === 'attachments', label: t('room.tab.attachments') },
      ]}
    />
  )
}
