import { toast } from 'sonner'
import { errorText } from '@/api/errorText'
import { useCancelReminder, type Reminder } from '@/api/reminders'
import type { Message } from '@/api/types'
import { useUsers } from '@/api/users'
import { Button } from '@/components/ui/button'
import { NoteLine } from '@/components/shared/note-line'
import { useT } from '@/lib/i18n'
import { systemNote } from './systemNote'

// ReminderNote is the note telling of a member setting a reminder
// (docs/design.md 5.23.4), with what a person may do about it: take it
// back while it is not yet due. Once due, taken back or dropped, it says
// which, as quiet as any other note.
export function ReminderNote({ message, reminder }: { message: Message; reminder: Reminder }) {
  const t = useT()
  const cancel = useCancelReminder()
  const users = useUsers()
  const note = systemNote(t, message.body)
  let state = ''
  switch (reminder.status) {
    case 'fired':
      state = t('reminder.fired')
      break
    case 'cancelled': {
      // Taken back by a person, or else by the member itself.
      const person = reminder.cancelled_by ? users.data?.find((u) => u.id === reminder.cancelled_by)?.name : undefined
      state = t('reminder.cancelled', { who: person ?? note.who[0] ?? '' })
      break
    }
    case 'dropped':
      state = t('reminder.dropped')
      break
  }
  return (
    <div className="mt-4 first:mt-1">
      <NoteLine note={note} time={message.created_at} settled={reminder.status !== 'pending'}>
        {reminder.status === 'pending' ? (
          <Button
            size="xs"
            variant="outline"
            disabled={cancel.isPending}
            onClick={() => cancel.mutate(reminder.id, { onError: (err) => toast.error(errorText(err)) })}
          >
            {t('reminder.cancel')}
          </Button>
        ) : (
          <span className="flex-none text-xs text-subtle">{state}</span>
        )}
      </NoteLine>
    </div>
  )
}
