import type { PermissionPreset } from '@/api/types'
import { t } from '@/lib/i18n'

export const presets: PermissionPreset[] = ['read_only', 'edit_with_approval', 'full_auto']

// A select cannot carry an empty value, so "follow the agent" travels
// through forms as this word and turns back into '' on the way out.
export const inheritPreset = 'inherit'

export function presetFromForm(value: FormDataEntryValue | null): PermissionPreset | '' {
  const raw = String(value ?? '')
  return raw === inheritPreset || raw === '' ? '' : (raw as PermissionPreset)
}

// presetLabel names a permission preset in the current language.
export function presetLabel(preset: PermissionPreset): string {
  return t(`preset.${preset}`)
}
