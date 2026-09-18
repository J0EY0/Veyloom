import { Link, Navigate, useParams } from 'react-router'
import { Panel } from '@/components/layout/Panel'
import { PanelHeader } from '@/components/layout/PanelHeader'
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from '@/components/ui/sidebar'
import { useT } from '@/lib/i18n'
import { useDocumentTitle } from '@/lib/useDocumentTitle'
import { cn } from '@/lib/utils'
import { AccountSettings } from './AccountSettings'
import { GeneralSettings } from './GeneralSettings'
import { settingsSections, type SectionId } from './sections'
import { ShortcutsSettings } from './ShortcutsSettings'

const content: Record<SectionId, () => React.JSX.Element> = {
  general: GeneralSettings,
  shortcuts: ShortcutsSettings,
  account: AccountSettings,
}

// The settings (docs/webui.md §4.8): a column of their own listing the
// kinds of setting, as the inbox lists its entries, next to the one picked;
// the app's sidebar stays as it is. /settings shows the first kind beside
// the column; on a phone it is the column alone, and a kind replaces it.
export function SettingsPage() {
  const t = useT()
  useDocumentTitle(t('settings.title'))
  const { section } = useParams()
  const picked = settingsSections.find((candidate) => candidate.id === section)
  if (section && !picked) return <Navigate to="/settings" replace />
  const current = picked ?? settingsSections[0]
  const Section = content[current.id]

  return (
    <Panel className="flex-row">
      <Sidebar collapsible="none" className={cn('w-full md:w-56 md:flex-none md:border-r', picked && 'hidden md:flex')}>
        <SidebarHeader className="p-0">
          <PanelHeader title={t('settings.title')} />
        </SidebarHeader>
        <SidebarContent>
          <SidebarGroup className="pt-0">
            <SidebarGroupContent>
              <nav aria-label={t('settings.nav')}>
                <SidebarMenu>
                  {settingsSections.map(({ id, icon: Icon, label }) => {
                    const active = id === current.id
                    return (
                      <SidebarMenuItem key={id}>
                        <SidebarMenuButton
                          asChild
                          isActive={active}
                          // On a phone the bare column has nothing open yet, so
                          // the first kind is not shown as picked there.
                          className={cn(
                            'h-8 text-[0.8125rem]',
                            active && 'text-foreground',
                            !picked && 'max-md:data-[active=true]:bg-transparent max-md:data-[active=true]:font-normal',
                          )}
                        >
                          <Link to={`/settings/${id}`} aria-current={active ? 'page' : undefined}>
                            <Icon className={active ? 'text-foreground' : 'text-subtle'} />
                            <span>{t(label)}</span>
                          </Link>
                        </SidebarMenuButton>
                      </SidebarMenuItem>
                    )
                  })}
                </SidebarMenu>
              </nav>
            </SidebarGroupContent>
          </SidebarGroup>
        </SidebarContent>
      </Sidebar>
      <div className={cn('min-w-0 flex-1 flex-col', picked ? 'flex' : 'hidden md:flex')}>
        <Section />
      </div>
    </Panel>
  )
}
