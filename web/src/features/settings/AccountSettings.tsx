import { useState } from 'react'
import { LogOutIcon } from 'lucide-react'
import { useLogout } from '@/api/auth'
import { UserAvatar } from '@/components/shared/user-avatar'
import { Button } from '@/components/ui/button'
import { Item, ItemActions, ItemContent, ItemMedia, ItemTitle } from '@/components/ui/item'
import { PasswordDialog } from '@/features/identity/PasswordDialog'
import { RenameUserDialog } from '@/features/identity/RenameUserDialog'
import { useCurrentUser } from '@/lib/currentUser'
import { useT } from '@/lib/i18n'
import { SettingRow, SettingsGroup, SettingsSection } from './SettingsLayout'

// The one account: its name and password, and the way out.
export function AccountSettings() {
  const t = useT()
  const user = useCurrentUser()
  const logout = useLogout()
  const [renaming, setRenaming] = useState(false)
  const [changing, setChanging] = useState(false)
  return (
    <SettingsSection title={t('settings.account')}>
      <SettingsGroup>
        <Item role="listitem" size="sm" className="min-h-16 gap-x-3 gap-y-2 py-3">
          <ItemMedia>
            <UserAvatar name={user?.name ?? '?'} size="lg" />
          </ItemMedia>
          <ItemContent className="min-w-0">
            <ItemTitle className="max-w-full truncate text-[0.9375rem]">{user?.name ?? t('user.loading')}</ItemTitle>
          </ItemContent>
          <ItemActions>
            <Button variant="outline" size="sm" disabled={!user} onClick={() => setRenaming(true)}>
              {t('settings.changeName')}
            </Button>
          </ItemActions>
        </Item>
        <SettingRow label={t('settings.password')}>
          <Button variant="outline" size="sm" disabled={!user} onClick={() => setChanging(true)}>
            {t('settings.changePassword')}
          </Button>
        </SettingRow>
      </SettingsGroup>
      <div>
        <Button variant="outline" size="sm" disabled={logout.isPending} onClick={() => logout.mutate()}>
          <LogOutIcon data-icon="inline-start" />
          {t('user.logout')}
        </Button>
      </div>
      {renaming && user ? <RenameUserDialog name={user.name} onClose={() => setRenaming(false)} /> : null}
      {changing ? <PasswordDialog onClose={() => setChanging(false)} /> : null}
    </SettingsSection>
  )
}
