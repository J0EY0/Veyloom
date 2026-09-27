import { toast } from 'sonner'
import { useCancelTurn } from '@/api/turns'
import { Button } from '@/components/ui/button'
import { quietFor } from '@/features/turns/quiet'
import { useT } from '@/lib/i18n'
import { useLiveTurn } from '@/lib/liveTurns'
import type { MemberState } from './useMemberStates'

// A working member whose turn went quiet (docs/design.md 5.23.8), as the
// island says it: what it was doing, for how long it has shown no sign of
// life, and the ways out, a person's to choose. The hub stops nothing
// itself: a long build is quiet too.
export function QuietLead({ state, now }: { state: MemberState; now: number }) {
  const t = useT()
  const live = useLiveTurn(state.turn?.id)
  const cancel = useCancelTurn()
  const turn = state.turn
  if (!turn?.quiet_since) return null
  const name = state.member.display_name
  return (
    <>
      <span className="min-w-0 truncate text-muted-foreground">
        <b className="font-medium text-foreground">{name}</b>
        {live?.tool ? (
          <>
            {' · '}
            <span className="font-mono text-[0.75rem]" translate="no">
              {live.tool}
            </span>
          </>
        ) : null}
        {' · '}
        <span className="text-status-wait">{quietFor(t, turn.quiet_since, now)}</span>
      </span>
      <Button size="xs" variant="ghost" disabled={cancel.isPending} onClick={() => cancel.mutate({ turnId: turn.id })}>
        {t('turn.cancel')}
      </Button>
      <Button
        size="xs"
        variant="outline"
        className="mr-1 rounded-full px-3"
        disabled={cancel.isPending}
        onClick={() => cancel.mutate({ turnId: turn.id, newSession: true }, { onSuccess: () => toast(t('member.newSessionDone', { name })) })}
      >
        {t('turn.cancelFresh')}
      </Button>
    </>
  )
}
