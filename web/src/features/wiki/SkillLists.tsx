import type { ReactNode } from 'react'
import { ChevronDownIcon } from 'lucide-react'
import { Link } from 'react-router'
import type { SkillUse, WikiPage } from '@/api/types'
import { skillName, skillUsesLimit, useSkillUses } from '@/api/wiki'
import { AgentAvatar } from '@/components/shared/agent-avatar'
import { Chip, ChipText } from '@/components/shared/chip'
import { StatusDot } from '@/components/shared/status-dot'
import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Spinner } from '@/components/ui/spinner'
import { useAgentLooks } from '@/lib/agentLooks'
import { formatTime } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { runtimeName } from '@/lib/runtimes'
import { InstallMenu } from './library/InstallMenu'
import { topicHref } from './links'

// The two facts of a skill that grow with use (docs/webui.md 4.10): the
// agents it is installed for, and the turns that used it. Each keeps to
// one line, a few chips and a chip for the rest, which opens the whole
// list beside it: the block above a skill's text stays the same height.

// The agents shown as chips before "+N".
const installsShown = 3
// The turns shown as chips before the chip for all of them.
const usesShown = 2

// SkillInstalls shows the agents a skill is installed for, each with its
// face in its runtime's tint, and installs it for more or takes it off.
export function SkillInstalls({ page }: { page: WikiPage }) {
  const t = useT()
  const lookOf = useAgentLooks()
  const installed = page.installed ?? []
  const lookFor = (id: string) => lookOf(id) ?? { avatar: '', runtime: '' }
  return (
    <>
      {installed.length === 0 ? <span>{t('skill.notInstalled')}</span> : null}
      {installed.slice(0, installsShown).map((agent) => (
        <Chip key={agent.id}>
          <AgentAvatar look={lookFor(agent.id)} name={agent.name} size="xs" mark={false} />
          <ChipText>{agent.name}</ChipText>
        </Chip>
      ))}
      {installed.length > installsShown ? (
        <MoreChip label={`+${installed.length - installsShown}`} name={t('skill.facts.allInstalled', { n: installed.length })}>
          <ListHead>{t('skill.facts.installedCount', { n: installed.length })}</ListHead>
          <ul aria-label={t('skill.facts.installed')} className="flex flex-col pb-1.5">
            {installed.map((agent) => (
              <li key={agent.id} className="flex min-w-0 items-center gap-2 px-3 py-1.5 text-[0.8125rem]">
                <AgentAvatar look={lookFor(agent.id)} name={agent.name} size="sm" mark={false} />
                <span className="truncate text-foreground">{agent.name}</span>
                <span className="ml-auto flex-none text-xs text-subtle">{runtimeName(lookFor(agent.id).runtime)}</span>
              </li>
            ))}
          </ul>
        </MoreChip>
      ) : null}
      <InstallMenu name={skillName(page.path)} skill={page}>
        <Button variant="ghost" size="xs" className="h-6 gap-1 px-1.5 text-muted-foreground">
          {t('skill.facts.manage')}
          <ChevronDownIcon className="size-3" />
        </Button>
      </InstallMenu>
    </>
  )
}

const tones: Record<string, 'ok' | 'fail' | 'run' | 'idle'> = { done: 'ok', failed: 'fail', running: 'run', cancelled: 'idle' }

// SkillUses shows the turns that used a skill, newest first: the latest
// two as chips, each with its result's dot, where it ran and who ran it,
// leading to its topic; and a chip for all of them, listed with when. At
// most skillUsesLimit are read, so a full list says there may be more.
export function SkillUses({ name }: { name: string }) {
  const t = useT()
  const uses = useSkillUses(name)
  if (uses.isPending) {
    return (
      <span role="status" className="inline-flex items-center gap-2 text-xs text-subtle">
        <Spinner className="size-3" />
        {t('common.loading')}
      </span>
    )
  }
  const list = uses.data ?? []
  if (list.length === 0) return <span>{t('skill.unused')}</span>
  const capped = list.length >= skillUsesLimit
  const count = capped ? t('skill.facts.usesCapped', { n: skillUsesLimit }) : t('skill.facts.allUses', { n: list.length })
  return (
    <>
      {list.slice(0, usesShown).map((use) => (
        <Chip key={use.turn_id} asChild className="pl-2">
          <Link to={topicHref(use.room_id, use.thread_id)}>
            <StatusDot tone={tones[use.status] ?? 'idle'} />
            <ChipText>{t('skill.use', { project: use.project_name, n: use.topic_number })}</ChipText>
            <AgentAvatar look={{ avatar: '', runtime: use.runtime }} name={use.member_name} size="xs" mark={false} />
            <span className="flex-none text-muted-foreground">{use.member_name}</span>
          </Link>
        </Chip>
      ))}
      {list.length > usesShown ? (
        <MoreChip label={count} name={`${t('skill.uses')} · ${count}`} wide>
          <ListHead>{t('skill.uses')}</ListHead>
          <ScrollArea className="[&>[data-slot=scroll-area-viewport]]:max-h-72">
            <ul aria-label={t('skill.uses')} className="flex flex-col pb-1.5">
              {list.map((use) => (
                <UseRow key={use.turn_id} use={use} />
              ))}
            </ul>
          </ScrollArea>
          {capped ? <p className="px-3 pt-1 pb-2.5 text-xs text-subtle">{t('skill.facts.usesCappedNote', { n: skillUsesLimit })}</p> : null}
        </MoreChip>
      ) : null}
    </>
  )
}

// One turn in the list of all: its result, where it ran, who, and when.
function UseRow({ use }: { use: SkillUse }) {
  const t = useT()
  return (
    <li className="flex min-w-0 items-center gap-2 px-3 py-1.5 text-[0.8125rem]">
      <StatusDot tone={tones[use.status] ?? 'idle'} />
      <Link to={topicHref(use.room_id, use.thread_id)} className="min-w-0 truncate text-foreground underline-offset-3 hover:underline">
        {t('skill.use', { project: use.project_name, n: use.topic_number })}
      </Link>
      <AgentAvatar look={{ avatar: '', runtime: use.runtime }} name={use.member_name} size="xs" mark={false} />
      <span className="flex-none text-muted-foreground">{use.member_name}</span>
      <time dateTime={use.started_at} className="ml-auto flex-none pl-2 text-xs text-subtle tabular-nums">
        {formatTime(use.started_at)}
      </time>
    </li>
  )
}

// MoreChip is the chip for the rest of a list, which opens all of it.
function MoreChip({ label, name, wide = false, children }: { label: string; name: string; wide?: boolean; children: ReactNode }) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Chip asChild className="px-2 text-muted-foreground">
          <button type="button" aria-label={name}>
            {label}
          </button>
        </Chip>
      </PopoverTrigger>
      {/* The wide list hangs from the chip's end, as the chip closes its
          row; both keep clear of the window's edges. */}
      <PopoverContent
        align={wide ? 'end' : 'start'}
        collisionPadding={16}
        className={`${wide ? 'w-[min(26rem,calc(100vw-2rem))]' : 'w-[min(18rem,calc(100vw-2rem))]'} p-0`}
      >
        {children}
      </PopoverContent>
    </Popover>
  )
}

function ListHead({ children }: { children: ReactNode }) {
  return <p className="px-3 pt-2.5 pb-1 text-xs font-medium text-subtle">{children}</p>
}
