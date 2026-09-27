import { useRunningTurns } from '@/api/turns'
import type { t as translate } from '@/lib/i18n'

type T = typeof translate

// A turn that may be stuck (docs/design.md 5.23.8): on its machine, waiting
// on no person, it showed no sign of life for a while. The hub says so; the
// UI says for how long, and leaves it to a person.

// quietFor says how long a quiet turn has shown no sign of life, to the
// minute: "12 分钟没有动静".
export function quietFor(t: T, since: string, now: number): string {
  const minutes = Math.max(1, Math.floor((now - new Date(since).getTime()) / 60_000))
  const hours = Math.floor(minutes / 60)
  const span =
    hours === 0
      ? t('quiet.minutes', { n: minutes })
      : minutes % 60 === 0
        ? t('quiet.hours', { n: hours })
        : t('quiet.hoursMinutes', { h: hours, m: minutes % 60 })
  return t('quiet.for', { span })
}

// useQuietSince is since when a running turn of the room showed no sign of
// life, once it went quiet; undefined while it shows some, and for a turn
// not running.
export function useQuietSince(roomId: string, turnId: string | undefined): string | undefined {
  const running = useRunningTurns(turnId ? roomId : '')
  return turnId ? running.data?.find((turn) => turn.id === turnId)?.quiet_since : undefined
}
