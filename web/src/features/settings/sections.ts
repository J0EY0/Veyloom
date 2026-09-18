import { KeyboardIcon, Settings2Icon, UserIcon, type LucideIcon } from 'lucide-react'
import type { MessageKey } from '@/i18n/zh-CN'

export type SectionId = 'general' | 'shortcuts' | 'account'

export interface SettingsSectionInfo {
  id: SectionId
  icon: LucideIcon
  label: MessageKey
}

// The kinds of setting, in the order the settings column lists them. The
// first is what /settings shows next to the column.
export const settingsSections: SettingsSectionInfo[] = [
  { id: 'general', icon: Settings2Icon, label: 'settings.general' },
  { id: 'shortcuts', icon: KeyboardIcon, label: 'settings.shortcuts' },
  { id: 'account', icon: UserIcon, label: 'settings.account' },
]
