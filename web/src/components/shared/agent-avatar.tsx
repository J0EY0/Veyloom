import { avatarUrl } from '@/api/avatars'
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar'
import type { AgentLook } from '@/lib/agentLooks'
import { cn } from '@/lib/utils'
import { RuntimeIcon } from './runtime-icon'
import { avatarSizes, type AvatarSize } from './user-avatar'

export interface AgentAvatarProps {
  look: AgentLook
  size?: AvatarSize
  className?: string
}

// An agent's face wherever it appears, at the sizes a person's initial
// comes in so the two line up: the picture uploaded for it, or its
// runtime's mark. While the picture loads, or if it is gone, the mark
// stands in.
export function AgentAvatar({ look, size = 'md', className }: AgentAvatarProps) {
  const box = cn(avatarSizes[size], className)
  if (!look.avatar) return <RuntimeIcon runtime={look.runtime} className={box} />
  return (
    <Avatar aria-hidden="true" data-avatar={look.avatar} className={cn('after:hidden', box)}>
      <AvatarImage src={avatarUrl(look.avatar)} alt="" className="object-cover" />
      <AvatarFallback className="bg-transparent">
        <RuntimeIcon runtime={look.runtime} className="size-full" />
      </AvatarFallback>
    </Avatar>
  )
}
