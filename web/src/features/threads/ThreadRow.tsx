import type { ReactNode } from 'react'
import { AgentAvatar } from '@/components/shared/agent-avatar'
import { UserAvatar } from '@/components/shared/user-avatar'
import { Badge } from '@/components/ui/badge'
import type { Sender } from '@/features/rooms/useSenderNames'
import { formatTime } from '@/lib/format'
import { runtimeName } from '@/lib/runtimes'

export interface ThreadRowProps {
  sender: Sender
  time?: string
  children: ReactNode
}

// One speaker inside a topic, drawn as the chat draws a message: the face
// on the left; the name, an agent's runtime and the time on top; what they
// said below.
export function ThreadRow({ sender, time, children }: ThreadRowProps) {
  return (
    <div className="mt-4 grid grid-cols-[1.25rem_minmax(0,1fr)] gap-x-2.5 first:mt-1">
      {sender.look ? <AgentAvatar look={sender.look} size="sm" /> : <UserAvatar name={sender.name} size="sm" />}
      <div className="min-w-0">
        <div className="flex h-5 items-center gap-2 text-[0.78125rem] leading-tight">
          <span className="truncate font-medium text-foreground">{sender.name}</span>
          {sender.runtime ? (
            <Badge variant="outline" className="h-4 rounded-[5px] px-1.5 text-[0.625rem] font-normal text-subtle" translate="no">
              {runtimeName(sender.runtime)}
            </Badge>
          ) : null}
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
