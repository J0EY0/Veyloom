import { useEffect, useRef } from 'react'
import { toast } from 'sonner'
import { StatusDot } from '@/components/shared/status-dot'
import { useConnectionStatus } from '@/lib/connection'
import { useT } from '@/lib/i18n'

// Says so while the live stream is down, and once more when it is back
// (docs/webui.md §5.3): the one place the connection shows.
export function ConnectionHint() {
  const status = useConnectionStatus()
  const previous = useRef(status)
  const t = useT()

  useEffect(() => {
    if (previous.current === 'reconnecting' && status === 'open') {
      toast.success(t('connection.reconnected'))
    }
    previous.current = status
  }, [status, t])

  if (status !== 'reconnecting') return null
  return (
    <span role="status" className="mr-1 inline-flex items-center gap-1.5 text-xs text-status-wait">
      <StatusDot tone="wait" />
      {t('connection.reconnecting')}
    </span>
  )
}
