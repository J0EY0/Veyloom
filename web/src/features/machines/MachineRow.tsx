import { ServerIcon } from 'lucide-react'
import { Link } from 'react-router'
import type { Machine } from '@/api/types'
import { Badge } from '@/components/ui/badge'
import { Item, ItemContent, ItemDescription, ItemMedia, ItemTitle } from '@/components/ui/item'
import { formatAgo } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { detectedRuntimes, needsYou } from './machines'

// One machine in the list: its name, that it is online and when it was
// last heard from, and a count when runtimes there need you. Only connected
// machines are listed, so each is online.
export function MachineRow({ machine, now, active }: { machine: Machine; now: number; active: boolean }) {
  const t = useT()
  const attention = detectedRuntimes(machine).filter((runtime) => needsYou(runtime.state)).length
  return (
    <Item asChild size="sm" className="flex-nowrap gap-3 rounded-lg border-transparent px-2 py-2 hover:bg-muted/60 data-active:bg-muted">
      <Link to={`/machines/${machine.id}`} aria-current={active ? 'page' : undefined} data-active={active || undefined}>
        <ItemMedia variant="icon" className="size-8 rounded-lg border-0 bg-muted text-muted-foreground">
          <ServerIcon />
        </ItemMedia>
        <ItemContent className="min-w-0 gap-0.5">
          <ItemTitle className="max-w-full truncate" translate="no">
            {machine.name}
          </ItemTitle>
          <ItemDescription className="truncate text-xs text-subtle">
            {t('machines.online')}
            {' · '}
            {t('machines.heartbeatAgo', { ago: formatAgo(machine.last_seen, new Date(now)) })}
          </ItemDescription>
        </ItemContent>
        {attention > 0 ? (
          <Badge
            aria-label={t('machines.summaryAttention', { n: attention })}
            className="h-4.5 min-w-4.5 flex-none rounded-full border-0 bg-status-wait/15 px-1.5 font-semibold text-status-wait tabular-nums"
          >
            {attention}
          </Badge>
        ) : null}
      </Link>
    </Item>
  )
}
