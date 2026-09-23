import { useLocation } from 'react-router'
import { HeaderTabs } from '@/components/layout/HeaderTabs'
import { useT } from '@/lib/i18n'
import { wikiHref } from '@/features/wiki/links'

export interface RoomTabsProps {
  roomId: string
  wiki: boolean
}

// The chat's views, in its top bar (docs/design.md 5.14): the chat and
// the project wiki. Switching keeps the open topic or panel open.
export function RoomTabs({ roomId, wiki }: RoomTabsProps) {
  const t = useT()
  const { search } = useLocation()
  return (
    <HeaderTabs
      label={t('room.views')}
      tabs={[
        { to: { pathname: `/rooms/${roomId}`, search }, active: !wiki, label: t('room.tab.chat') },
        { to: { pathname: wikiHref(roomId), search }, active: wiki, label: t('room.tab.wiki') },
      ]}
    />
  )
}
