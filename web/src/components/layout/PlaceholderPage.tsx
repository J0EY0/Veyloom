import { Empty, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { useDocumentTitle } from '@/lib/useDocumentTitle'
import { Panel } from './Panel'
import { PanelHeader } from './PanelHeader'
import { useT } from '@/lib/i18n'
import type { MessageKey } from '@/i18n/zh-CN'

export interface PlaceholderPageProps {
  titleKey: MessageKey
}

// The page for an address that has nothing behind it.
export function PlaceholderPage({ titleKey }: PlaceholderPageProps) {
  const t = useT()
  const title = t(titleKey)
  useDocumentTitle(title)
  return (
    <Panel>
      <PanelHeader title={title} />
      <Empty>
        <EmptyHeader>
          <EmptyTitle>{title}</EmptyTitle>
        </EmptyHeader>
      </Empty>
    </Panel>
  )
}
