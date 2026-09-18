import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { cn } from '@/lib/utils'

export const avatarSizes = {
  lg: 'size-9 text-[0.8125rem] rounded-lg',
  md: 'size-6 text-[0.6875rem]',
  sm: 'size-5 text-[0.625rem]',
  xs: 'size-4 text-[0.5rem]',
} as const

export type AvatarSize = keyof typeof avatarSizes

export interface UserAvatarProps {
  name: string
  size?: AvatarSize
  className?: string
}

// A neutral disc with the first letter: people and agents look the same,
// the name next to it tells them apart (docs/webui.md §0).
export function UserAvatar({ name, size = 'md', className }: UserAvatarProps) {
  return (
    <Avatar aria-hidden="true" className={cn('after:hidden', avatarSizes[size], className)}>
      <AvatarFallback className="bg-secondary font-semibold text-muted-foreground" style={{ fontSize: 'inherit' }}>
        {initialOf(name)}
      </AvatarFallback>
    </Avatar>
  )
}

export function initialOf(name: string): string {
  const first = name.trim().charAt(0)
  return first === '' ? '?' : first.toUpperCase()
}
