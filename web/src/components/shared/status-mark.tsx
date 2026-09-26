import { cn } from '@/lib/utils'

export type StatusMarkKind = 'run' | 'merge' | 'done' | 'wait'

// Colour by state: under way amber, waiting to be merged violet, done
// green, waiting on a person orange (docs/webui.md 4.20).
const colours: Record<StatusMarkKind, string> = {
  run: 'text-status-run',
  merge: 'text-status-merge',
  done: 'text-status-ok',
  wait: 'text-status-wait',
}

// StatusMark is where a task stands, the way Linear marks it: a ring half
// filled while under way, three quarters while it waits to be merged, a
// filled check once done, a pause while a request waits on a person. The
// words go beside it; the mark itself is not read out.
export function StatusMark({ kind, className }: { kind: StatusMarkKind; className?: string }) {
  return (
    <svg viewBox="0 0 14 14" aria-hidden="true" className={cn('size-3.5 flex-none', colours[kind], className)}>
      {kind === 'run' ? (
        <>
          <circle cx="7" cy="7" r="5.5" fill="none" stroke="currentColor" strokeWidth="1.5" />
          <path d="M7 3.5 A3.5 3.5 0 0 1 7 10.5 Z" fill="currentColor" />
        </>
      ) : kind === 'merge' ? (
        <>
          <circle cx="7" cy="7" r="5.5" fill="none" stroke="currentColor" strokeWidth="1.5" />
          <path d="M7 3.5 A3.5 3.5 0 1 1 3.5 7 L7 7 Z" fill="currentColor" />
        </>
      ) : kind === 'done' ? (
        <>
          <circle cx="7" cy="7" r="6.25" fill="currentColor" />
          <path d="M4.4 7.2 6.2 9 9.7 5.2" fill="none" stroke="var(--background)" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
        </>
      ) : (
        <>
          <circle cx="7" cy="7" r="6.25" fill="currentColor" />
          <rect x="4.6" y="4.4" width="1.6" height="5.2" rx="0.6" fill="var(--background)" />
          <rect x="7.8" y="4.4" width="1.6" height="5.2" rx="0.6" fill="var(--background)" />
        </>
      )}
    </svg>
  )
}
