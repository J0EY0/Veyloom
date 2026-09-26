import type { ReactNode } from 'react'
import { XIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

export interface SidePanelProps {
  // The accessible name of the panel.
  label: string
  // What the header shows before the close button.
  header: ReactNode
  // A line under the header: how what the panel shows stands.
  subheader?: ReactNode
  // A strip across the panel under the header, for a state that holds
  // until someone ends it.
  band?: ReactNode
  // Extra header buttons, before the close button.
  headerActions?: ReactNode
  onClose: () => void
  closeLabel: string
  // Fills the middle column instead of floating over its right side.
  wide?: boolean
  // A list of names needs less room than something you read.
  narrow?: boolean
  // 'pane' fills its column instead of floating: the detail side of a
  // list, like the topic in the inbox.
  variant?: 'floating' | 'pane'
  footer?: ReactNode
  children: ReactNode
  className?: string
}

// The second card of the room: a topic, the members or the pending
// approvals. It sits on the right of the chat, under the room's top bar;
// whether the chat makes room for it or it lies over the chat is the
// room's call (docs/webui.md §0). Everything inside scrolls between a fixed
// header and an optional footer.
export function SidePanel({
  label,
  header,
  subheader,
  band,
  headerActions,
  onClose,
  closeLabel,
  wide,
  narrow,
  variant = 'floating',
  footer,
  children,
  className,
}: SidePanelProps) {
  const pane = variant === 'pane'
  return (
    <aside
      aria-label={label}
      className={cn(
        pane
          ? 'flex h-full min-w-0 flex-1 flex-col overflow-hidden'
          : 'absolute top-0 right-3 bottom-3 z-10 flex flex-col overflow-hidden rounded-xl border bg-popover shadow-elev transition-[width] duration-200',
        !pane && (wide ? 'w-[calc(100%-1.5rem)]' : narrow ? 'w-75 max-w-[calc(100%-1.5rem)]' : 'w-102 max-w-[calc(100%-1.5rem)]'),
        className,
      )}
    >
      <header className={cn('flex flex-none items-center gap-2 pr-2.5 pl-4', pane ? 'h-12' : 'h-13')}>
        {header}
        <span className="grow" />
        {headerActions}
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon-sm" aria-label={closeLabel} onClick={onClose} className="text-subtle">
              <XIcon />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">{closeLabel}</TooltipContent>
        </Tooltip>
      </header>
      {subheader ? <div className="-mt-1.5 flex-none px-4 pb-3">{subheader}</div> : null}
      {band}
      <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 pb-4">{children}</div>
      {footer}
    </aside>
  )
}
