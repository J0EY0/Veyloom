import { StatusDot, type StatusTone } from '@/components/shared/status-dot'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

// The dot alone reads as status only next to its own label; once a label
// sits somewhere else on the row it needs the tint to carry across
// (docs/webui.md §0). Same five tones, same colours, on shadcn's Badge.
const tones: Record<StatusTone, string> = {
  idle: 'bg-secondary text-muted-foreground',
  run: 'bg-status-run/12 text-status-run',
  wait: 'bg-status-wait/12 text-status-wait',
  ok: 'bg-status-ok/12 text-status-ok',
  fail: 'bg-status-fail/12 text-status-fail',
}

export interface StatusPillProps {
  tone: StatusTone
  children: React.ReactNode
  // A dot inside the pill, for where the tint alone is too quiet.
  dot?: boolean
  className?: string
}

export function StatusPill({ tone, children, dot = true, className }: StatusPillProps) {
  return (
    <Badge variant="secondary" className={cn('gap-1.5 rounded-md px-1.5 text-[0.6875rem] leading-4', tones[tone], className)}>
      {dot ? <StatusDot tone={tone} className="opacity-90" /> : null}
      {children}
    </Badge>
  )
}
