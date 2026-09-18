import { Kbd, KbdGroup } from '@/components/ui/kbd'
import type { MessageKey } from '@/i18n/zh-CN'
import { useT } from '@/lib/i18n'
import { SettingRow, SettingsGroup, SettingsSection } from './SettingsLayout'

interface Shortcut {
  label: MessageKey
  keys: string[]
}

// The shortcuts the app answers to today, by where they work. Keep this in
// step with the handlers: the command palette (⌘K), shadcn's sidebar (⌘B),
// useEscape, the composer (↵, ⇧↵, @) and the Agents page (⌘F).
const groups: { title: MessageKey; shortcuts: Shortcut[] }[] = [
  {
    title: 'shortcuts.general',
    shortcuts: [
      { label: 'shortcuts.search', keys: ['⌘', 'K'] },
      { label: 'shortcuts.sidebar', keys: ['⌘', 'B'] },
      { label: 'shortcuts.close', keys: ['Esc'] },
    ],
  },
  {
    title: 'shortcuts.chat',
    shortcuts: [
      { label: 'shortcuts.send', keys: ['↵'] },
      { label: 'shortcuts.newline', keys: ['⇧', '↵'] },
      { label: 'shortcuts.mention', keys: ['@'] },
    ],
  },
  {
    title: 'shortcuts.agents',
    shortcuts: [{ label: 'shortcuts.findAgent', keys: ['⌘', 'F'] }],
  },
]

export function ShortcutsSettings() {
  const t = useT()
  return (
    <SettingsSection title={t('settings.shortcuts')}>
      {groups.map((group) => (
        <SettingsGroup key={group.title} title={t(group.title)}>
          {group.shortcuts.map((shortcut) => (
            <SettingRow key={shortcut.label} label={t(shortcut.label)}>
              <KbdGroup>
                {shortcut.keys.map((key) => (
                  <Kbd key={key} className="min-w-6 font-sans">
                    {key}
                  </Kbd>
                ))}
              </KbdGroup>
            </SettingRow>
          ))}
        </SettingsGroup>
      ))}
    </SettingsSection>
  )
}
