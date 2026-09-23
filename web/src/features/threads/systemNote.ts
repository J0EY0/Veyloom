import type { MessageKey } from '@/i18n/zh-CN'
import type { t as translate } from '@/lib/i18n'

type T = typeof translate

// The system notes the hub writes in English that the UI says its own way;
// the rest are shown as the hub wrote them.
// An upkeep's note (docs/design.md 5.12, 5.16): who, why, and what it goes
// over; what people said came in later, so older notes lack it.
const upkeepNote =
  /^Wiki upkeep by (.+) \((topics went quiet|daily|every three days|weekly|the last upkeep left turns to go over|asked by a person)\): (\d+) turns? of this chat, (\d+) turns? of other projects using this team's skills(?:, (\d+) messages? from people)?\.$/

const upkeepReasons: Record<string, MessageKey> = {
  'topics went quiet': 'upkeep.reason.quiet',
  daily: 'upkeep.reason.daily',
  'every three days': 'upkeep.reason.every3days',
  weekly: 'upkeep.reason.weekly',
  'the last upkeep left turns to go over': 'upkeep.reason.backlog',
  'asked by a person': 'upkeep.reason.asked',
}

// systemText is a system note in the UI's words.
export function systemText(t: T, body: string): string {
  const upkeep = upkeepNote.exec(body)
  if (upkeep) {
    const note = t('upkeep.note', { who: upkeep[1], reason: t(upkeepReasons[upkeep[2]]), own: upkeep[3], uses: upkeep[4] })
    return upkeep[5] !== undefined ? note + t('upkeep.notePeople', { people: upkeep[5] }) : note
  }
  return body
}
