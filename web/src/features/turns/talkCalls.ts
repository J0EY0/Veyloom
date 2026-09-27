import { spanMs } from '@/features/threads/systemNote'
import { formatLongSpan, formatTime } from '@/lib/format'
import { t } from '@/lib/i18n'

// What a call of the tools a member talks with asks for, in a line: when a
// reminder comes due and what for (docs/design.md 5.23.4), what a draft has
// a person do (5.23.5).

// talkCall says what a call of a reminder or draft tool asks for, by the
// tool's bare name; undefined for any other tool.
export function talkCall(name: string, input: string): string | undefined {
  switch (name) {
    case 'set_reminder':
      return reminderCall(argsOf(input))
    case 'cancel_reminder':
      return t('tool.cancelReminder')
    case 'draft_action':
      return draftCall(argsOf(input))
  }
  return undefined
}

type Args = Record<string, unknown>

function argsOf(input: string): Args {
  try {
    const parsed: unknown = JSON.parse(input)
    return parsed !== null && typeof parsed === 'object' ? (parsed as Args) : {}
  } catch {
    // Not JSON: say what it asked for without it.
    return {}
  }
}

function text(args: Args, key: string): string {
  return typeof args[key] === 'string' ? (args[key] as string).trim() : ''
}

function reminderCall(args: Args): string {
  const note = text(args, 'note')
  const at = text(args, 'at')
  if (at !== '' && !Number.isNaN(Date.parse(at))) return t('tool.remindAt', { time: formatTime(at), note })
  const span = text(args, 'in').replace(/\s+/g, '')
  return t('tool.remindIn', { span: spanMs(span) > 0 ? formatLongSpan(spanMs(span)) : span, note })
}

function draftCall(args: Args): string {
  // With no member named, the member drafts about its own work.
  const member = text(args, 'member').replace(/^@/, '')
  const skill = text(args, 'skill')
  switch (text(args, 'kind')) {
    case 'merge':
      return member ? t('tool.draft.merge', { member }) : t('tool.draft.mergeOwn')
    case 'set_aside':
      return member ? t('tool.draft.setAside', { member }) : t('tool.draft.setAsideOwn')
    case 'install_skill':
      return member ? t('tool.draft.skill', { member, skill }) : t('tool.draft.skillOwn', { skill })
  }
  return t('tool.draft.other')
}
