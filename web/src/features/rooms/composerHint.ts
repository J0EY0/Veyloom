import type { Addressee } from '@/api/types'
import type { t as translate } from '@/lib/i18n'

export interface ComposerHint {
  text: string
  // The words ask for an @: the key follows them.
  mention?: boolean
}

// composerHint is the line under the composer: whom a message that names
// no member goes to, as the hub answers (docs/design.md 4.2), or, when it
// would go to nobody, that an @ reaches a member. When the hub could not
// say, or the member's name is not known: the keys in a topic, the @ in
// the room.
export function composerHint(to: Addressee | undefined, names: ReadonlyMap<string, string>, inTopic: boolean, t: typeof translate): ComposerHint {
  const mention: ComposerHint = { text: t('composer.hintMention'), mention: true }
  if (to?.reason === 'none') return mention
  const name = to?.member_id ? names.get(to.member_id) : undefined
  if (to && name) {
    switch (to.reason) {
      case 'leader':
        return { text: t('composer.leaderHint', { name }) }
      case 'leader_fallback':
        return { text: t('composer.leaderFallbackHint', { name }) }
      default:
        return { text: t('composer.replyHint', { name }) }
    }
  }
  return inTopic ? { text: t('composer.hintKeys') } : mention
}
