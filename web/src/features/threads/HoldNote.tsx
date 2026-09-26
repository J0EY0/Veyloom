import { toast } from 'sonner'
import { errorText } from '@/api/errorText'
import { useContinueRelay, type RelayHold } from '@/api/relays'
import type { Message } from '@/api/types'
import { Button } from '@/components/ui/button'
import { useT } from '@/lib/i18n'
import { NoteLine } from '@/components/shared/note-line'
import { systemNote } from './systemNote'

// HoldNote is a note telling of a wake a limit on agents waking one another
// held back (docs/design.md 5.22), with what the person does about it: let
// it go on, which starts a piece of work of its own. Let go on, it is as
// quiet as any other note.
export function HoldNote({ message, hold }: { message: Message; hold: RelayHold }) {
  const t = useT()
  const go = useContinueRelay(message.thread_id ?? '')
  const settled = hold.continued_at !== undefined && hold.continued_at !== null
  return (
    <div className="mt-4 first:mt-1">
      <NoteLine note={systemNote(t, message.body)} time={message.created_at} settled={settled}>
        {settled ? (
          <span className="flex-none text-xs text-subtle">{t('relay.continued')}</span>
        ) : (
          <Button size="xs" variant="outline" disabled={go.isPending} onClick={() => go.mutate(message.id, { onError: (err) => toast.error(errorText(err)) })}>
            {t('relay.continue')}
          </Button>
        )}
      </NoteLine>
    </div>
  )
}
