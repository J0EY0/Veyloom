import { RefreshCwIcon } from 'lucide-react'
import { toast } from 'sonner'
import { ProbeTimeout, useProbeMachines } from '@/api/agents'
import type { Machine } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { useT } from '@/lib/i18n'

// "Check again": after installing or updating something, the machines
// look for their runtimes again. The button spins until they have answered.
export function RecheckButton({ machines }: { machines: Machine[] }) {
  const t = useT()
  const probe = useProbeMachines()

  function recheck() {
    probe.mutate(machines, {
      onError: (err) => {
        toast.error(
          err instanceof ProbeTimeout ? t('machines.recheckTimeout', { names: err.names.join('、') }) : t('machines.recheckFailed', { error: err.message }),
        )
      },
    })
  }

  return (
    <Button variant="outline" size="sm" onClick={recheck} disabled={probe.isPending}>
      {probe.isPending ? <Spinner data-icon="inline-start" aria-hidden="true" /> : <RefreshCwIcon data-icon="inline-start" />}
      {probe.isPending ? t('machines.rechecking') : t('machines.recheck')}
    </Button>
  )
}
