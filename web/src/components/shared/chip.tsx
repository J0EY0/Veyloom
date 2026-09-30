import type { ComponentProps, ReactNode } from 'react'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

export type ChipProps = ComponentProps<typeof Badge>

// A capsule for a face or a mark and a name, drawn as an @-mention is and
// a little larger: the agents, teams and turns a skill's facts list
// (docs/webui.md 4.10). One that leads somewhere lights up under the
// pointer. It is never wider than its row: its words, in a ChipText, end
// in an ellipsis instead. What else it is given, a popover trigger's
// handlers among them, goes on to the Badge.
export function Chip({ className, children, ...props }: ChipProps) {
  return (
    <Badge
      {...props}
      variant="secondary"
      className={cn(
        'h-6 max-w-full min-w-0 shrink justify-start gap-1.5 rounded-full pr-2 pl-1 text-[0.8125rem] font-normal text-foreground [a&]:transition-colors [a&]:hover:bg-selection [button&]:transition-colors [button&]:hover:bg-selection',
        className,
      )}
    >
      {children}
    </Badge>
  )
}

// ChipText is the words of a chip, cut short when the row is narrow.
export function ChipText({ className, children }: { className?: string; children: ReactNode }) {
  return <span className={cn('min-w-0 truncate', className)}>{children}</span>
}
