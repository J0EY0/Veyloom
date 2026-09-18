import { cn } from '@/lib/utils'

export type StatusTone = 'idle' | 'run' | 'wait' | 'ok' | 'fail'

const tones: Record<StatusTone, string> = {
  idle: 'bg-subtle',
  run: 'bg-status-run animate-breathe',
  wait: 'bg-status-wait',
  ok: 'bg-status-ok',
  fail: 'bg-status-fail',
}

// The 6px dot that carries every status colour in the UI (docs/webui.md §0).
export function StatusDot({ tone, className }: { tone: StatusTone; className?: string }) {
  return <span aria-hidden="true" className={cn('inline-block size-1.5 flex-none rounded-full', tones[tone], className)} />
}
