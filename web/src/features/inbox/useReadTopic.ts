import { useEffect } from 'react'
import { useMarkInboxRead } from '@/api/inbox'
import type { Message } from '@/api/types'
import { useCurrentUser } from '@/lib/currentUser'

// useReadTopic marks read, while a topic is open, what in it mentions the
// person (docs/webui.md 4.19): what is addressed to them is read there, and
// their inbox counts it no more. It asks again as more of it arrives while
// the topic stays open.
export function useReadTopic(threadId: string, messages: (Pick<Message, 'mentions'> | undefined)[]) {
  const user = useCurrentUser()
  const me = user?.id ?? ''
  const { mutate } = useMarkInboxRead(me)
  const mine = messages.filter((message) => message?.mentions?.some((m) => m.kind === 'user' && m.id === me)).length
  useEffect(() => {
    if (me !== '' && threadId !== '' && mine > 0) mutate({ thread_id: threadId })
  }, [me, threadId, mine, mutate])
}
