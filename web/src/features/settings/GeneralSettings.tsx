import type { ReactNode } from 'react'
import { MonitorIcon, MoonIcon, SunIcon, type LucideIcon } from 'lucide-react'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useWidthRem } from '@/features/rooms/panelLayout'
import { locales, setLocale, useLocale, useT, type Locale } from '@/lib/i18n'
import { setTheme, useTheme, type Theme } from '@/lib/theme'
import { setUiSize, uiSizes, useUiSize } from '@/lib/uiSize'
import { cn } from '@/lib/utils'
import { MemorySettings } from './MemorySettings'
import { SettingRow, SettingsGroup, SettingsSection } from './SettingsLayout'

const themeIcons = { system: MonitorIcon, light: SunIcon, dark: MoonIcon } as const
const themes: Theme[] = ['system', 'light', 'dark']

// Each language names itself, so a reader lost in the wrong one can find
// their own.
const languageNames: Record<Locale, string> = { 'zh-CN': '中文', en: 'English' }

// How wide each setting's choices are side by side, measured in English,
// whose names run longest, with the row's own padding (2rem): a row
// narrower than that, a phone's at a large interface size, picks from a
// menu instead.
const THEME_ROW_REM = 18.5
const SIZE_ROW_REM = 15.75
const LANGUAGE_ROW_REM = 11

// The general settings, how the app looks and reads: the colour scheme,
// the interface size and the language. Each is few enough choices to show
// at once, and each takes effect as it is picked. Then the memories the
// members' turns use (docs/design.md 5.19).
export function GeneralSettings() {
  const t = useT()
  const theme = useTheme()
  const size = useUiSize()
  const locale = useLocale()
  // Every row is as wide as the first.
  const [rowRef, rowRem] = useWidthRem()
  const narrow = (rem: number) => rowRem > 0 && rowRem < rem
  return (
    <SettingsSection title={t('settings.general')}>
      <SettingsGroup>
        <SettingRow ref={rowRef} label={t('settings.theme')}>
          <Choice
            label={t('settings.theme')}
            value={theme}
            onChange={setTheme}
            menu={narrow(THEME_ROW_REM)}
            options={themes.map((option) => ({ value: option, label: t(`theme.${option}`), icon: themeIcons[option] }))}
          />
        </SettingRow>
        <SettingRow label={t('uiSize.title')}>
          <Choice
            label={t('uiSize.title')}
            value={size}
            onChange={setUiSize}
            menu={narrow(SIZE_ROW_REM)}
            options={uiSizes.map((option) => ({ value: option, label: t(`uiSize.${option}`) }))}
          />
        </SettingRow>
        <SettingRow label={t('settings.interfaceLanguage')}>
          <Choice
            label={t('settings.interfaceLanguage')}
            value={locale}
            onChange={setLocale}
            menu={narrow(LANGUAGE_ROW_REM)}
            options={locales.map((option) => ({ value: option, label: languageNames[option], lang: option }))}
          />
        </SettingRow>
      </SettingsGroup>
      <MemorySettings />
    </SettingsSection>
  )
}

interface ChoiceOption<V extends string> {
  value: V
  label: ReactNode
  icon?: LucideIcon
  lang?: string
}

// One setting's few choices, side by side; in a row too narrow for them, a
// menu of the same.
function Choice<V extends string>({
  label,
  value,
  onChange,
  options,
  menu,
}: {
  label: string
  value: V
  onChange: (value: V) => void
  options: ChoiceOption<V>[]
  menu: boolean
}) {
  if (menu) {
    return (
      <Select value={value} onValueChange={(next) => onChange(next as V)}>
        <SelectTrigger size="sm" aria-label={label}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent align="end">
          {options.map(({ value: option, label: name, icon: Icon, lang }) => (
            <SelectItem key={option} value={option} lang={lang}>
              {Icon ? <Icon /> : null}
              {name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    )
  }
  return (
    <Tabs value={value} onValueChange={(next) => onChange(next as V)}>
      <TabsList aria-label={label}>
        {options.map(({ value: option, label: name, icon: Icon, lang }) => (
          <TabsTrigger key={option} value={option} lang={lang} className={cn(Icon ? 'px-3' : 'px-3.5')}>
            {Icon ? <Icon /> : null}
            {name}
          </TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  )
}
