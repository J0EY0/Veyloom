import { EllipsisIcon, LockIcon, PencilIcon, ShieldCheckIcon, ShieldIcon, Trash2Icon, UnlockIcon } from 'lucide-react'
import type { Agent, PermissionPreset } from '@/api/types'
import { AgentAvatar } from '@/components/shared/agent-avatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Skeleton } from '@/components/ui/skeleton'
import { runtimeName } from '@/lib/runtimes'
import { cn } from '@/lib/utils'
import { useT } from '@/lib/i18n'

// The name pill wears the runtime's colour, the same hue as the mark in
// the corner. Full class names, so Tailwind's scanner sees them.
const pillOf: Record<string, string> = {
  claude: 'bg-runtime-claude-wash text-runtime-claude-ink',
  codex: 'bg-runtime-codex-wash text-runtime-codex-ink',
  pi: 'bg-runtime-pi-wash text-runtime-pi-ink',
}

// Permission reads as a door: shut, guarded by a person, guarded by the
// runtime's own reviewer, open.
const gates: Record<PermissionPreset, { Icon: typeof LockIcon; hot?: boolean }> = {
  read_only: { Icon: LockIcon },
  edit_with_approval: { Icon: ShieldIcon },
  auto_review: { Icon: ShieldCheckIcon },
  full_auto: { Icon: UnlockIcon, hot: true },
}

export interface AgentCardProps {
  agent: Agent
  onOpen: (agent: Agent) => void
  onDelete: (agent: Agent) => void
}

// One agent on the Agents page (docs/webui.md §4.7). The whole card opens
// it; the "…" in the corner is its own button beside that one (a button
// cannot sit inside another) with edit and delete. Only the card's own
// button rings the card when focused.
export function AgentCard({ agent, onOpen, onDelete }: AgentCardProps) {
  const t = useT()
  const summary = summaryOf(agent.role_card)
  const gate = gates[agent.permission_preset]

  return (
    <Card className="relative h-full gap-0 overflow-hidden py-0 transition-colors hover:border-input has-[[data-card-open]:focus-visible]:border-ring has-[[data-card-open]:focus-visible]:ring-[3px] has-[[data-card-open]:focus-visible]:ring-ring/50">
      <button type="button" data-card-open onClick={() => onOpen(agent)} className="flex h-full w-full flex-col gap-2 p-3.5 text-left outline-none">
        <span className="flex min-w-0 items-center gap-2 pr-6">
          <AgentAvatar look={agent} name={agent.name} />
          <Badge
            variant="secondary"
            className={cn(
              'block min-w-0 shrink truncate px-2.5 text-[0.8125rem] font-semibold tracking-[-0.008em]',
              pillOf[agent.runtime] ?? 'bg-secondary text-foreground',
            )}
          >
            {agent.name}
          </Badge>
        </span>
        {summary ? <p className="line-clamp-2 text-xs leading-relaxed text-body">{summary}</p> : null}
        <span className="mt-auto flex items-center gap-3 pt-3.5 text-[0.6875rem] text-subtle">
          {agent.model ? (
            <span className="truncate font-mono" translate="no">
              {agent.model}
            </span>
          ) : null}
          <span className={cn('inline-flex flex-none items-center gap-1.5', gate.hot && 'text-status-fail')}>
            <gate.Icon className="size-3.5" />
            {t(`preset.${agent.permission_preset}`)}
          </span>
          {/* The avatar's corner shows the runtime's mark; this says it in words. */}
          <span className="sr-only">{runtimeName(agent.runtime)}</span>
        </span>
      </button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="icon-xs"
            aria-label={t('common.more')}
            className="absolute top-3.5 right-2.5 text-subtle hover:text-foreground data-[state=open]:text-foreground"
          >
            <EllipsisIcon className="size-3.5" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-36">
          <DropdownMenuItem onSelect={() => onOpen(agent)}>
            <PencilIcon />
            {t('common.edit')}
          </DropdownMenuItem>
          <DropdownMenuItem variant="destructive" onSelect={() => onDelete(agent)}>
            <Trash2Icon />
            {t('common.delete')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </Card>
  )
}

// A card's shape while the list loads. Only the first one is announced.
export function AgentCardSkeleton({ label }: { label?: string }) {
  return (
    <li aria-hidden={label ? undefined : 'true'} role={label ? 'status' : undefined} aria-label={label}>
      <Card className="gap-0 py-0">
        <div className="flex flex-col gap-2 p-3.5">
          <Skeleton className="h-5 w-2/3 rounded-full" />
          <Skeleton className="h-3 w-full" />
          <Skeleton className="h-3 w-4/5" />
          <div className="flex items-center gap-3 pt-3.5">
            <Skeleton className="h-3 w-12" />
            <Skeleton className="h-3 w-14" />
            <Skeleton className="ml-auto size-6 rounded-full" />
          </div>
        </div>
      </Card>
    </li>
  )
}

// The role card is markdown written for the runtime; its opening line,
// stripped of syntax, says more about the agent than any field.
function summaryOf(roleCard: string): string {
  if (!roleCard) return ''
  return roleCard
    .replace(/[#*`>_[\]]/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
}
