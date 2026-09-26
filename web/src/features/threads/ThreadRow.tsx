import type { ReactNode } from 'react'
import { AgentAvatar } from '@/components/shared/agent-avatar'
import { UserAvatar } from '@/components/shared/user-avatar'
import type { Sender } from '@/features/rooms/useSenderNames'
import { formatTime } from '@/lib/format'

export interface ThreadRowProps {
  sender: Sender
  time?: string
  // A word on what the speaker is doing, after its runtime: a wiki upkeep.
  label?: string
  children: ReactNode
}

// One speaker inside a topic, drawn as the chat draws a message: the face,
// whose corner says an agent's runtime, on the left; the name and the time
// on top; what they said below.
export function ThreadRow({ sender, time, label, children }: ThreadRowProps) {
  return (
    <div className="mt-4 grid grid-cols-[1.75rem_minmax(0,1fr)] gap-x-3 first:mt-1">
      {sender.look ? <AgentAvatar look={sender.look} name={sender.name} size="message" /> : <UserAvatar name={sender.name} size="message" />}
      <div className="min-w-0">
        <div className="flex h-7 items-center gap-2 text-[0.84375rem] leading-tight">
          <span className="truncate font-semibold text-foreground">{sender.name}</span>
          {label ? <span className="flex-none text-xs text-subtle">{label}</span> : null}
          {time ? (
            <time dateTime={time} className="flex-none text-xs text-subtle">
              {formatTime(time)}
            </time>
          ) : null}
        </div>
        {children}
      </div>
    </div>
  )
}
