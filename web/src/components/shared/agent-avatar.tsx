import { avatarUrl } from '@/api/avatars'
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar'
import type { AgentLook } from '@/lib/agentLooks'
import { cn } from '@/lib/utils'
import { RuntimeIcon } from './runtime-icon'
import { avatarSizes, initialOf, type AvatarSize } from './user-avatar'

export interface AgentAvatarProps {
  look: AgentLook
  // Whose face: its first letter stands in when no picture was uploaded.
  // Without it the runtime's mark does.
  name?: string
  size?: AvatarSize
  // The runtime's mark in the corner; by default on the sizes it reads at.
  mark?: boolean
  className?: string
}

// A runtime's tint for the disc an agent's letter sits in (docs/webui.md
// §0): its wash behind, its ink for the letter, a hairline of its colour.
const tints: Record<string, string> = {
  claude: 'bg-runtime-claude-wash text-runtime-claude-ink ring-runtime-claude/30',
  codex: 'bg-runtime-codex-wash text-runtime-codex-ink ring-runtime-codex/30',
  pi: 'bg-runtime-pi-wash text-runtime-pi-ink ring-runtime-pi/30',
}

const marked: ReadonlySet<AvatarSize> = new Set(['lg', 'message', 'md'])

// An agent's face wherever it appears, round and at the sizes a person's
// initial comes in so the two line up: the picture uploaded for it, or
// else its first letter on a disc tinted by its runtime, so that two agents
// on one runtime are told apart; the runtime's own mark sits in the corner.
// While the picture loads, or if it is gone, the letter stands in.
export function AgentAvatar({ look, name, size = 'md', mark, className }: AgentAvatarProps) {
  const box = cn(avatarSizes[size], className)
  if (!look.avatar && name === undefined) return <RuntimeIcon runtime={look.runtime} className={box} />
  const letter = (
    <span
      aria-hidden="true"
      className={cn(
        'inline-flex size-full items-center justify-center rounded-full font-semibold ring-1 ring-inset',
        tints[look.runtime] ?? 'bg-secondary text-muted-foreground ring-border',
      )}
    >
      {initialOf(name ?? look.runtime)}
    </span>
  )
  const face = look.avatar ? (
    <Avatar aria-hidden="true" data-avatar={look.avatar} className="size-full after:hidden">
      <AvatarImage src={avatarUrl(look.avatar)} alt="" className="object-cover" />
      <AvatarFallback className="bg-transparent" style={{ fontSize: 'inherit' }}>
        {letter}
      </AvatarFallback>
    </Avatar>
  ) : (
    letter
  )
  // Round as the face is, so that a ring drawn around the avatar (a shadow
  // in className) follows it.
  return (
    <span aria-hidden="true" className={cn('relative inline-flex flex-none rounded-full', box)}>
      {face}
      {(mark ?? marked.has(size)) ? (
        <RuntimeIcon runtime={look.runtime} className="absolute -right-[0.1875rem] -bottom-[0.1875rem] size-3 ring-2 ring-background" />
      ) : null}
    </span>
  )
}
