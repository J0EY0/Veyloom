import type { PermissionPreset } from '@/api/types'
import { Field, FieldContent, FieldDescription, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { inheritPreset, presetHint, presetLabel, presets } from './presets'

export interface PresetFieldProps {
  // Prefixes the ids that tie each choice to its label.
  idPrefix: string
  // The member's own preset; '' follows the agent's.
  defaultValue: PermissionPreset | ''
  // The agent's preset, which following it means, once known.
  agentPreset?: PermissionPreset
}

// PresetField picks what a member may do without asking anyone: what its
// agent may, or one of the four presets, each said in a line (docs/design.md
// 4.6). It reaches its form as permission_preset.
export function PresetField({ idPrefix, defaultValue, agentPreset }: PresetFieldProps) {
  const t = useT()
  const choices = [
    { value: inheritPreset, label: t('member.followAgent'), hint: agentPreset ? t('member.followAgentHint', { preset: presetLabel(agentPreset) }) : undefined },
    ...presets.map((preset) => ({ value: preset, label: presetLabel(preset), hint: presetHint(preset) })),
  ]
  return (
    <FieldSet className="gap-2">
      <FieldLegend variant="label" className="mb-0">
        {t('member.permission')}
      </FieldLegend>
      <RadioGroup name="permission_preset" defaultValue={defaultValue || inheritPreset} className="gap-0 overflow-hidden rounded-lg border">
        {choices.map((choice, index) => {
          const id = `${idPrefix}-${choice.value}`
          return (
            <Field key={choice.value} orientation="horizontal" className={cn('gap-3 px-3 py-2 has-data-[state=checked]:bg-muted/60', index > 0 && 'border-t')}>
              <RadioGroupItem id={id} value={choice.value} />
              <FieldContent className="gap-0">
                <FieldLabel htmlFor={id} className="text-[0.8125rem]">
                  {choice.label}
                </FieldLabel>
                {choice.hint ? <FieldDescription className="text-xs leading-snug">{choice.hint}</FieldDescription> : null}
              </FieldContent>
            </Field>
          )
        })}
      </RadioGroup>
    </FieldSet>
  )
}
