import { t } from '@/lib/i18n'

// turnErrorText is how a turn ended badly, in the reader's language when the
// hub wrote it: the machine gone before the turn could start, or while it
// ran. A runtime's own words stay as they are.
export function turnErrorText(error: string): string {
  if (error === 'machine is offline') return t('turn.error.offline')
  if (error === 'machine disconnected') return t('turn.error.disconnected')
  const dispatch = /^dispatch to machine: (.+)$/s.exec(error)
  if (dispatch) return t('turn.error.dispatch', { error: dispatch[1] })
  return error
}
