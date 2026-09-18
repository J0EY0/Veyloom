import { Fragment, type ReactNode } from 'react'
import { ItemGroup, ItemSeparator } from '@/components/ui/item'
import { Skeleton } from '@/components/ui/skeleton'

// A bordered list whose rows are split by hairlines; a row's hover fill
// stays inside the rounded border.
export function Rows({ children }: { children: ReactNode[] }) {
  return (
    <ItemGroup className="overflow-hidden rounded-xl border bg-card">
      {children.map((child, index) => (
        <Fragment key={index}>
          {index > 0 ? <ItemSeparator /> : null}
          {child}
        </Fragment>
      ))}
    </ItemGroup>
  )
}

// Rows in the shape of the real ones while the list loads.
export function RowsSkeleton({ label }: { label: string }) {
  return (
    <div role="status" aria-label={label} className="flex flex-col gap-5 rounded-xl border p-4">
      {[0, 1].map((row) => (
        <div key={row} className="flex items-center gap-3">
          <Skeleton className="size-8 rounded-lg" />
          <div className="flex flex-col gap-2">
            <Skeleton className="h-3.5 w-40" />
            <Skeleton className="h-3 w-56" />
          </div>
        </div>
      ))}
    </div>
  )
}
