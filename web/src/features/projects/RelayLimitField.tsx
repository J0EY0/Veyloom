import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { useT } from '@/lib/i18n'

export interface RelayLimitFieldProps {
  id: string
  // The limit as typed, and whether there is none.
  value: string
  unlimited: boolean
  onValue: (value: string) => void
  onUnlimited: (unlimited: boolean) => void
}

// RelayLimitField sets how many turns agents may wake one another to in a
// piece of work a person started, before it waits for them (docs/design.md
// 5.22): a number of turns, 0 for none at all, or no limit.
export function RelayLimitField({ id, value, unlimited, onValue, onUnlimited }: RelayLimitFieldProps) {
  const t = useT()
  return (
    <Field>
      <FieldLabel htmlFor={`${id}-relay`}>{t('project.relayLimit')}</FieldLabel>
      <div className="flex items-center gap-2">
        <Input
          id={`${id}-relay`}
          type="number"
          inputMode="numeric"
          min={0}
          max={999}
          value={unlimited ? '' : value}
          disabled={unlimited}
          onChange={(event) => onValue(event.target.value)}
          className="w-20 tabular-nums"
        />
        <span className="text-sm text-subtle">{t('project.relayLimitUnit')}</span>
        <div className="ml-auto flex items-center gap-2">
          <Switch id={`${id}-relay-off`} checked={unlimited} onCheckedChange={onUnlimited} />
          <Label htmlFor={`${id}-relay-off`} className="font-normal">
            {t('project.relayUnlimited')}
          </Label>
        </div>
      </div>
      <FieldDescription>{t('project.relayLimitHint')}</FieldDescription>
    </Field>
  )
}

// relayValue is what the field shows of a project's relay limit: the
// turns, 0 when agents wake no one (stored as a negative), and the default
// to start from when there is no limit.
export function relayValue(limit: number | undefined): string {
  if (limit === undefined || limit === 0) return '30'
  return String(Math.max(limit, 0))
}

// relayChange is what the dialog asks to change of the relay limit: the
// turns typed, up to 999, 0 kept as -1 for agents waking no one; 0 for no
// limit. Nothing when it stays as it was, or nothing is typed.
export function relayChange(current: number | undefined, value: string, unlimited: boolean): { relay_limit?: number } {
  let next = 0
  if (!unlimited) {
    const typed = Number.parseInt(value, 10)
    if (Number.isNaN(typed)) return {}
    next = Math.min(Math.max(typed, 0), 999) || -1
  }
  return next === (current ?? 30) ? {} : { relay_limit: next }
}
