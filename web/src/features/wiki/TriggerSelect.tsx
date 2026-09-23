import type { UpkeepTrigger } from '@/api/types'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import type { MessageKey } from '@/i18n/zh-CN'
import { useT } from '@/lib/i18n'

// When a wiki maintainer goes over the chat, most often first: a week at
// the longest (docs/design.md 5.16).
const triggers: { value: UpkeepTrigger; key: MessageKey }[] = [
  { value: 'idle', key: 'maintainer.trigger.idle' },
  { value: 'daily', key: 'maintainer.trigger.daily' },
  { value: 'every_3_days', key: 'maintainer.trigger.every3days' },
  { value: 'weekly', key: 'maintainer.trigger.weekly' },
  { value: 'manual', key: 'maintainer.trigger.manual' },
]

// The same, as a phrase in a sentence.
export const triggerWhenKeys: Record<UpkeepTrigger, MessageKey> = {
  idle: 'maintainer.when.idle',
  daily: 'maintainer.when.daily',
  every_3_days: 'maintainer.when.every3days',
  weekly: 'maintainer.when.weekly',
  manual: 'maintainer.when.manual',
}

export interface TriggerSelectProps {
  id?: string
  value: UpkeepTrigger
  // How long a topic stays quiet, for the first choice's words.
  idleMinutes: number
  onChange: (value: UpkeepTrigger) => void
  disabled?: boolean
  size?: 'sm' | 'default'
  className?: string
  label?: string
}

// TriggerSelect picks when the maintainer runs.
export function TriggerSelect({ id, value, idleMinutes, onChange, disabled, size, className, label }: TriggerSelectProps) {
  const t = useT()
  return (
    <Select value={value} onValueChange={(next) => onChange(next as UpkeepTrigger)} disabled={disabled}>
      <SelectTrigger id={id} size={size} className={className} aria-label={label}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {triggers.map((trigger) => (
          <SelectItem key={trigger.value} value={trigger.value}>
            {t(trigger.key, { n: idleMinutes })}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
