import { ServerIcon } from 'lucide-react'
import { useParams } from 'react-router'
import { useMachines } from '@/api/agents'
import { Panel } from '@/components/layout/Panel'
import { PanelHeader } from '@/components/layout/PanelHeader'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { useT } from '@/lib/i18n'
import { useDocumentTitle } from '@/lib/useDocumentTitle'
import { useNow } from '@/lib/useNow'
import { MachineDetail, MachineGone, MachinePick } from './MachineDetail'
import { MachineList } from './MachineList'

// The machines page (docs/webui.md §4.7): every machine connected to the
// hub. One bar across the top, then the machines on the left and
// the open one beside them, with its own check again. Nothing opens until a
// machine is picked, as in the inbox: on a wide screen the pane beside the
// list says to pick one. On a phone the list and a machine take turns, as
// /machines and /machines/:machineId.
export function MachinesPage() {
  const { machineId = '' } = useParams()
  const t = useT()
  const machines = useMachines()
  const list = machines.data ?? []
  const selected = machineId ? list.find((machine) => machine.id === machineId) : undefined
  useDocumentTitle(machineId && selected ? selected.name : t('machines.title'))
  const now = useNow(list.length > 0, 10_000)
  const header = <PanelHeader title={t('machines.title')} />

  if (machines.isError || (machines.data && list.length === 0)) {
    return (
      <Panel>
        {header}
        <Empty>
          <EmptyHeader>
            {machines.isError ? null : (
              <EmptyMedia variant="icon">
                <ServerIcon />
              </EmptyMedia>
            )}
            <EmptyTitle>{machines.isError ? t('machines.failed') : t('machines.empty')}</EmptyTitle>
            {machines.isError ? <EmptyDescription>{machines.error.message}</EmptyDescription> : null}
          </EmptyHeader>
        </Empty>
      </Panel>
    )
  }
  const detailClass = machineId ? 'flex' : 'hidden md:flex'
  return (
    <Panel>
      {header}
      <div className="flex min-h-0 flex-1 border-t">
        <MachineList machines={machines.data} selectedId={selected?.id ?? ''} now={now} className={machineId ? 'hidden md:flex' : undefined} />
        {machines.isPending ? null : !machineId ? (
          <MachinePick className={detailClass} />
        ) : selected ? (
          <MachineDetail key={selected.id} machine={selected} now={now} className={detailClass} />
        ) : (
          <MachineGone className={detailClass} />
        )}
      </div>
    </Panel>
  )
}
