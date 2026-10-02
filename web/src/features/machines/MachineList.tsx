import type { Machine } from '@/api/types'
import { ItemGroup } from '@/components/ui/item'
import { Skeleton } from '@/components/ui/skeleton'
import { useT } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { MachineRow } from './MachineRow'

export interface MachineListProps {
  // Absent while the list loads.
  machines?: Machine[]
  selectedId: string
  now: number
  className?: string
}

// The machines, on the left under the page's bar: how many, then one row
// each, the open one filled in.
export function MachineList({ machines, selectedId, now, className }: MachineListProps) {
  const t = useT()
  return (
    <nav
      aria-label={t('machines.title')}
      className={cn('flex w-full flex-col gap-1 overflow-y-auto p-2.5 @split/panel:w-72 @split/panel:flex-none @split/panel:border-r', className)}
    >
      {machines ? (
        <>
          <p className="px-2 pt-0.5 pb-1.5 text-xs text-subtle">{t('machines.machineCount', { n: machines.length })}</p>
          <ItemGroup className="gap-1">
            {machines.map((machine) => (
              <div key={machine.id} role="listitem">
                <MachineRow machine={machine} now={now} active={machine.id === selectedId} />
              </div>
            ))}
          </ItemGroup>
        </>
      ) : (
        <div role="status" aria-label={t('common.loading')} className="flex flex-col gap-3 p-2">
          <Skeleton className="h-3 w-16" />
          {[0, 1].map((row) => (
            <div key={row} className="flex items-center gap-3">
              <Skeleton className="size-8 rounded-lg" />
              <div className="flex flex-col gap-2">
                <Skeleton className="h-3.5 w-32" />
                <Skeleton className="h-3 w-40" />
              </div>
            </div>
          ))}
        </div>
      )}
    </nav>
  )
}
