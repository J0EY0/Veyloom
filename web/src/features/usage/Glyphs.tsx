// The small pictures on the usage page's figure cards (docs/webui.md
// 4.20): a line of what they add up to, a meter, a strip of turns. Grey,
// the latest point in blue. Drawn in a box of their own that scales.

// Sparkline is values as a line, the last one a blue dot.
export function Sparkline({ values }: { values: number[] }) {
  const width = 104
  const height = 30
  if (values.length < 2) return <span className="block h-7.5 w-26 max-w-full" />
  const top = Math.max(...values) || 1
  const points = values.map((v, i) => [(i / (values.length - 1)) * (width - 6) + 1, height - 3 - (v / top) * (height - 8)] as const)
  const path = points.map(([x, y], i) => `${i === 0 ? 'M' : 'L'}${x.toFixed(1)} ${y.toFixed(1)}`).join(' ')
  const [lx, ly] = points[points.length - 1]
  return (
    <svg viewBox={`0 0 ${width} ${height}`} aria-hidden="true" className="h-7.5 w-26 max-w-full overflow-visible">
      <path d={path} fill="none" className="stroke-chart-mark" strokeWidth="1.5" strokeLinejoin="round" strokeLinecap="round" />
      <circle cx={lx} cy={ly} r="3.5" className="fill-chart-accent stroke-card" strokeWidth="2" />
    </svg>
  )
}

// Meter is a share as a bar filled that far.
export function Meter({ share }: { share: number }) {
  return (
    <span aria-hidden="true" className="block h-1.5 w-26 max-w-full overflow-hidden rounded-full bg-chart-track">
      <span className="block h-full rounded-full bg-chart-accent" style={{ width: `${Math.min(1, Math.max(0, share)) * 100}%` }} />
    </span>
  )
}

// Strip is a tick a turn, as many as fit.
export function Strip({ count }: { count: number }) {
  const shown = Math.min(count, 32)
  return (
    <span aria-hidden="true" className="flex h-4 w-26 max-w-full items-end gap-0.5">
      {Array.from({ length: shown }, (_, i) => (
        <span key={i} className="h-3.5 flex-1 rounded-[0.0625rem] bg-chart-mark" />
      ))}
    </span>
  )
}
