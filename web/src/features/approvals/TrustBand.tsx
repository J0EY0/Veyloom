import { ShieldCheckIcon } from 'lucide-react'
import { toast } from 'sonner'
import { useTurnApprovals } from '@/api/approvals'
import { errorText } from '@/api/errorText'
import { useUntrustTurn } from '@/api/turns'
import type { Turn } from '@/api/types'
import { Button } from '@/components/ui/button'
import { formatTime } from '@/lib/format'
import { useT } from '@/lib/i18n'

export interface TrustBandProps {
  // A running turn a person let the rest of its requests through.
  turn: Turn & { trusted_at: string }
  // Whose turn, when the topic has more than one trusted at once.
  name?: string
}

// TrustBand is the strip atop a topic while the rest of a turn's requests
// go through without asking anyone (docs/design.md 4.6): how many have,
// since when, and the button that has people asked again.
export function TrustBand({ turn, name }: TrustBandProps) {
  const t = useT()
  const approvals = useTurnApprovals(turn.id)
  const untrust = useUntrustTurn()
  const count = (approvals.data ?? []).filter((a) => a.reviewer === 'turn').length
  return (
    <div role="status" aria-label={t('trust.label')} className="flex flex-none items-center gap-2 border-y bg-muted/60 py-1.5 pr-2.5 pl-4 text-xs">
      <ShieldCheckIcon aria-hidden="true" className="size-3.5 flex-none text-status-ok" />
      <span className="min-w-0 truncate font-medium text-foreground">
        {name ? `${name} · ` : ''}
        {t('trust.label')}
      </span>
      <span className="grow" />
      <span className="flex-none text-subtle tabular-nums">{t('trust.count', { n: count, time: formatTime(turn.trusted_at) })}</span>
      <Button
        variant="outline"
        size="xs"
        className="flex-none"
        disabled={untrust.isPending}
        aria-busy={untrust.isPending}
        onClick={() => untrust.mutate(turn.id, { onError: (err) => toast.error(errorText(err)) })}
      >
        {t('trust.revoke')}
      </Button>
    </div>
  )
}

// trustedTurns are a topic's turns running with the rest of their requests
// let through.
export function trustedTurns(turns: readonly Turn[]): (Turn & { trusted_at: string })[] {
  return turns.filter((turn): turn is Turn & { trusted_at: string } => turn.status === 'running' && Boolean(turn.trusted_by && turn.trusted_at))
}
