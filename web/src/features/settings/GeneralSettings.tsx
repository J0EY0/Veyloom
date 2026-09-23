import { MonitorIcon, MoonIcon, SunIcon } from 'lucide-react'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { locales, setLocale, useLocale, useT, type Locale } from '@/lib/i18n'
import { setTheme, useTheme, type Theme } from '@/lib/theme'
import { setUiSize, uiSizes, useUiSize, type UiSize } from '@/lib/uiSize'
import { MemorySettings } from './MemorySettings'
import { SettingRow, SettingsGroup, SettingsSection } from './SettingsLayout'

const themeIcons = { system: MonitorIcon, light: SunIcon, dark: MoonIcon } as const
const themes: Theme[] = ['system', 'light', 'dark']

// Each language names itself, so a reader lost in the wrong one can find
// their own.
const languageNames: Record<Locale, string> = { 'zh-CN': '中文', en: 'English' }

// The general settings, how the app looks and reads: the colour scheme,
// the interface size and the language. Each is few enough choices to show
// at once, and each takes effect as it is picked. Then the memories the
// members' turns use (docs/design.md 5.19).
export function GeneralSettings() {
  const t = useT()
  const theme = useTheme()
  const size = useUiSize()
  const locale = useLocale()
  return (
    <SettingsSection title={t('settings.general')}>
      <SettingsGroup>
        <SettingRow label={t('settings.theme')}>
          <Tabs value={theme} onValueChange={(value) => setTheme(value as Theme)}>
            <TabsList aria-label={t('settings.theme')}>
              {themes.map((option) => {
                const Icon = themeIcons[option]
                return (
                  <TabsTrigger key={option} value={option} className="px-3">
                    <Icon />
                    {t(`theme.${option}`)}
                  </TabsTrigger>
                )
              })}
            </TabsList>
          </Tabs>
        </SettingRow>
        <SettingRow label={t('uiSize.title')}>
          <Tabs value={size} onValueChange={(value) => setUiSize(value as UiSize)}>
            <TabsList aria-label={t('uiSize.title')}>
              {uiSizes.map((option) => (
                <TabsTrigger key={option} value={option} className="px-3.5">
                  {t(`uiSize.${option}`)}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
        </SettingRow>
        <SettingRow label={t('settings.interfaceLanguage')}>
          <Tabs value={locale} onValueChange={(value) => setLocale(value as Locale)}>
            <TabsList aria-label={t('settings.interfaceLanguage')}>
              {locales.map((option) => (
                <TabsTrigger key={option} value={option} lang={option} className="px-3.5">
                  {languageNames[option]}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
        </SettingRow>
      </SettingsGroup>
      <MemorySettings />
    </SettingsSection>
  )
}
