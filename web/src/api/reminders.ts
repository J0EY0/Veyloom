import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { api } from './client'
import { refresh } from './refresh'

// A member's reminders to itself (docs/design.md 5.23.4): the hub wakes
// it, in the topic it set one in, once it comes due. The notes in the
// topic that tell of one being set are drawn by how it stands.

export type ReminderStatus = 'pending' | 'fired' | 'cancelled' | 'dropped'

export interface Reminder {
  id: string
  member_id: string
  room_id: string
  thread_id: string
  // The turn that set it.
  turn_id?: string
  note: string
  due_at: string
  // pending: not yet due; fired: came due; cancelled: taken back, by the
  // person cancelled_by names or else by the member; dropped: its member
  // was taken out of the project or switched off by the time it was due.
  status: ReminderStatus
  // The note that told of it being set, and the message it came due as.
  set_message_id?: string
  fired_message_id?: string
  cancelled_by?: string
  created_at: string
  settled_at?: string
}

export const reminderKeys = {
  all: ['reminders'] as const,
  thread: (threadId: string) => ['reminders', threadId] as const,
}

// useThreadReminders reads the reminders set in a topic; only asked for
// where a note tells of one.
export function useThreadReminders(threadId: string, enabled: boolean) {
  return useQuery({
    queryKey: reminderKeys.thread(threadId),
    queryFn: async () => (await api.get<{ reminders: Reminder[] }>(`/threads/${threadId}/reminders`)).reminders,
    enabled: enabled && threadId !== '',
  })
}

// applyReminder writes a reminder as it stands now into its topic's list,
// where one is read: set, come due, or taken back.
export function applyReminder(client: QueryClient, reminder: Reminder) {
  const key = reminderKeys.thread(reminder.thread_id)
  if (client.getQueryData(key) === undefined) return
  client.setQueryData<Reminder[]>(key, (list = []) =>
    list.some((r) => r.id === reminder.id) ? list.map((r) => (r.id === reminder.id ? reminder : r)) : [...list, reminder],
  )
}

// useCancelReminder takes back a reminder not yet due.
export function useCancelReminder() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (id: string) => (await api.delete<{ reminder: Reminder }>(`/reminders/${id}`)).reminder,
    onSuccess: (reminder) => applyReminder(client, reminder),
    onError: () => refresh(client, { queryKey: reminderKeys.all }),
  })
}
