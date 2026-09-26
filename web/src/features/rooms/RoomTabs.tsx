import { useLocation } from 'react-router'
import { HeaderTabs } from '@/components/layout/HeaderTabs'
import { useT } from '@/lib/i18n'
import { wikiHref } from '@/features/wiki/links'

// RoomView is which of the chat's views is open.
export type RoomView = 'chat' | 'wiki' | 'branches'

export interface RoomTabsProps {
  roomId: string
  view: RoomView
}

// The chat's views, in its top bar (docs/design.md 5.14, 5.21): the chat,
// the project wiki and the branches. Switching keeps the open topic or
// panel open.
export function RoomTabs({ roomId, view }: RoomTabsProps) {
  const t = useT()
  const { search } = useLocation()
  return (
    <HeaderTabs
      label={t('room.views')}
      tabs={[
        { to: { pathname: `/rooms/${roomId}`, search }, active: view === 'chat', label: t('room.tab.chat') },
        { to: { pathname: wikiHref(roomId), search }, active: view === 'wiki', label: t('room.tab.wiki') },
        { to: { pathname: `/rooms/${roomId}/branches`, search }, active: view === 'branches', label: t('room.tab.branches') },
      ]}
    />
  )
}
