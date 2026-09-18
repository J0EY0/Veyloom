import type { TokenUsage } from '@/api/types'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { useT } from '@/lib/i18n'
import { formatTokens, tokenParts, totalTokens } from '@/lib/tokens'
import { cn } from '@/lib/utils'

export interface TokenCountProps {
  usage: TokenUsage
  className?: string
}

// How many tokens something spent, short, with what they were made of on
// hover or focus: cache reads can be most of a total, and the total alone
// would hide that.
export function TokenCount({ usage, className }: TokenCountProps) {
  useT()
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          tabIndex={0}
          className={cn(
            'cursor-default rounded-sm whitespace-nowrap tabular-nums underline decoration-dotted decoration-from-font underline-offset-2 outline-none focus-visible:ring-2 focus-visible:ring-ring/50',
            className,
          )}
        >
          {formatTokens(totalTokens(usage))}
        </span>
      </TooltipTrigger>
      <TooltipContent side="top">{tokenParts(usage).join(' · ')}</TooltipContent>
    </Tooltip>
  )
}
