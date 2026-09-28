import type { WorkSummary } from '@/api/types'
import type { StatusTone } from '@/components/shared/status-dot'
import { formatDuration } from '@/lib/format'
import type { t as translate } from '@/lib/i18n'

// workState says how a piece of work stands (docs/design.md 5.22): under
// way, or over as its last turn to end ended, done, failed or cancelled,
// with how many turns it took and for how long. What went wrong in a turn
// is read in the topic, under that turn.
export function workState(work: WorkSummary, t: typeof translate): { tone: StatusTone; text: string } {
  if (work.running) return { tone: 'run', text: t('topic.workRunning', { turns: work.turns }) }
  const took = work.ended_at ? t('topic.took', { duration: formatDuration(work.started_at, work.ended_at) }) : ''
  const once = work.turns <= 1
  switch (work.last_status) {
    case 'failed':
      return { tone: 'fail', text: once ? t('topic.workFailedOnce', { took }) : t('topic.workFailed', { turns: work.turns, took }) }
    case 'cancelled':
      return { tone: 'idle', text: once ? t('topic.workCancelledOnce', { took }) : t('topic.workCancelled', { turns: work.turns, took }) }
    default:
      return { tone: 'ok', text: once ? t('topic.doneOnce', { took }) : t('topic.done', { turns: work.turns, took }) }
  }
}
