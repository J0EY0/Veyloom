import type { ReactNode } from 'react'
import { ShieldCheckIcon, ShieldIcon, ShieldQuestionMarkIcon, type LucideIcon } from 'lucide-react'
import { StatusPill } from '@/components/shared/status-pill'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { tierName } from './names'

// Facts are what a page carries besides its text, one row each: a small
// icon and a label, then the value. A skill's stand above its text, a wiki
// page's at its foot (docs/webui.md 4.10, 4.9). Values are chips where they
// name agents, teams, turns or tags, and colour is only where the app gives
// it a meaning: an agent's runtime, a turn's result, a person's review.
export function Facts({ className, children }: { className?: string; children: ReactNode }) {
  return <dl className={cn('grid grid-cols-[6.5rem_minmax(0,1fr)] gap-x-4 gap-y-2 text-[0.8125rem]', className)}>{children}</dl>
}

export function Fact({ icon: Icon, label, children }: { icon: LucideIcon; label: string; children: ReactNode }) {
  return (
    <>
      <dt className="flex h-6 items-center gap-1.5 text-xs text-subtle">
        <Icon className="size-3.5 flex-none" aria-hidden="true" />
        {label}
      </dt>
      <dd className="flex min-h-6 min-w-0 flex-wrap items-center gap-1.5 text-muted-foreground">{children}</dd>
    </>
  )
}

const trustIcons: Record<string, LucideIcon> = {
  unverified: ShieldQuestionMarkIcon,
  'machine-confirmed': ShieldIcon,
  'human-reviewed': ShieldCheckIcon,
}

// TrustPill says how far to trust a page: green once a person reviewed it,
// quiet before, its icon telling unverified from checked by a process. Not
// the wait colour: that is for what waits on a person, a trial among them.
export function TrustPill({ tier }: { tier: string }) {
  const t = useT()
  const Icon = trustIcons[tier] ?? ShieldIcon
  return (
    <StatusPill tone={tier === 'human-reviewed' ? 'ok' : 'idle'} dot={false} className="h-6 gap-1 rounded-full px-2 text-xs">
      <Icon className="size-3.5" aria-hidden="true" />
      {tierName(t, tier)}
    </StatusPill>
  )
}
