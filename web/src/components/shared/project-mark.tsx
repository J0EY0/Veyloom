import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { cn } from '@/lib/utils'
import { initialOf } from './user-avatar'

const sizes = {
  sm: { box: 'size-4 rounded-[4px]', letter: 'rounded-[4px] text-[0.5625rem]' },
  lg: { box: 'size-9 rounded-lg', letter: 'rounded-lg text-sm' },
} as const

export interface ProjectMarkProps {
  name: string
  // Filled in, for the project that is open.
  active?: boolean
  size?: keyof typeof sizes
  className?: string
}

// A project's first letter on a small square beside its name: shadcn's
// Avatar squared off, where a person's is round.
export function ProjectMark({ name, active = false, size = 'sm', className }: ProjectMarkProps) {
  return (
    <Avatar aria-hidden="true" className={cn(sizes[size].box, 'after:hidden', className)}>
      <AvatarFallback className={cn(sizes[size].letter, 'bg-secondary font-semibold text-muted-foreground', active && 'bg-primary text-primary-foreground')}>
        {initialOf(name)}
      </AvatarFallback>
    </Avatar>
  )
}
