import { BlocksIcon, ChevronDownIcon, UsersIcon } from 'lucide-react'
import { Link } from 'react-router'
import { toast } from 'sonner'
import { useProjects } from '@/api/projects'
import type { WikiPage, WikiTeam } from '@/api/types'
import { skillName, useChangeWikiPage, useSkillUses, librarySpace } from '@/api/wiki'
import { StatusDot } from '@/components/shared/status-dot'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Spinner } from '@/components/ui/spinner'
import { formatTime } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { runtimeName } from '@/lib/runtimes'
import { InstallMenu } from './library/InstallMenu'
import { topicHref } from './links'
import { errorText } from '@/api/errorText'

// What only a skill of the library has (docs/design.md 5.10, 5.15): the
// team that owns it, which a person may hand it on from; the agents it is
// installed for; and the turns that used it, which that team looks back on.

// SkillTeam says who owns a skill and hands it to another team.
export function SkillTeam({ page, teams }: { page: WikiPage; teams: WikiTeam[] }) {
  const t = useT()
  const projects = useProjects()
  const change = useChangeWikiPage(librarySpace)
  const owner = teams.find((team) => team.slug === page.team)
  const others = (projects.data ?? []).filter((project) => project.id !== owner?.project_id)

  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-subtle">
      <UsersIcon className="size-3.5" aria-hidden="true" />
      {owner ? (
        <span>{t('skill.ownedBy', { team: owner.name })}</span>
      ) : (
        <span className="text-status-wait">{page.team ? t('skill.teamGone', { team: page.team }) : t('skill.unowned')}</span>
      )}
      {others.length > 0 ? (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="xs" className="-my-1 h-6 gap-1 px-1.5 text-muted-foreground" disabled={change.isPending}>
              {t('skill.handOver')}
              <ChevronDownIcon className="size-3" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start">
            <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">{t('skill.handOverTo')}</DropdownMenuLabel>
            {others.map((project) => (
              <DropdownMenuItem
                key={project.id}
                onSelect={() =>
                  change.mutate(
                    { path: page.path, team: project.id },
                    {
                      onSuccess: () => toast.success(t('skill.handedOver', { team: project.name })),
                      onError: (err) => toast.error(t('wiki.page.changeFailed', { error: errorText(err) })),
                    },
                  )
                }
              >
                {project.name}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      ) : null}
    </div>
  )
}

// SkillInstalls says whom a skill is installed for, and installs it for
// more agents or takes it off.
export function SkillInstalls({ page }: { page: WikiPage }) {
  const t = useT()
  const installed = page.installed ?? []
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-subtle">
      <BlocksIcon className="size-3.5" aria-hidden="true" />
      <span>{installed.length > 0 ? t('skill.installedFor', { agents: installed.map((a) => a.name).join(t('common.listSeparator')) }) : t('skill.notInstalled')}</span>
      <InstallMenu name={skillName(page.path)} skill={page}>
        <Button variant="ghost" size="xs" className="-my-1 h-6 gap-1 px-1.5 text-muted-foreground">
          {t('skill.install')}
          <ChevronDownIcon className="size-3" />
        </Button>
      </InstallMenu>
    </div>
  )
}

const tones: Record<string, 'ok' | 'fail' | 'run' | 'idle'> = { done: 'ok', failed: 'fail', running: 'run', cancelled: 'idle' }

// SkillUses lists the turns that used a skill, newest first, each leading
// to the topic it ran in.
export function SkillUses({ name }: { name: string }) {
  const t = useT()
  const uses = useSkillUses(name)
  return (
    <section aria-labelledby="skill-uses" className="mt-8">
      <h2 id="skill-uses" className="mb-1.5 text-xs font-medium text-subtle">
        {t('skill.uses')}
      </h2>
      {uses.isPending ? (
        <p role="status" className="flex items-center gap-2 py-1 text-xs text-subtle">
          <Spinner className="size-3" />
          {t('common.loading')}
        </p>
      ) : !uses.data || uses.data.length === 0 ? (
        <p className="text-[0.8125rem] text-subtle">{t('skill.unused')}</p>
      ) : (
        <ul className="grid gap-1 text-[0.8125rem]">
          {uses.data.map((use) => (
            <li key={use.turn_id} className="flex min-w-0 items-center gap-2">
              <StatusDot tone={tones[use.status] ?? 'idle'} />
              <Link to={topicHref(use.room_id, use.thread_id)} className="truncate underline-offset-3 hover:underline">
                {t('skill.use', { project: use.project_name, n: use.topic_number })}
              </Link>
              <span className="flex-none text-muted-foreground">
                {use.member_name} · {runtimeName(use.runtime)}
              </span>
              <time dateTime={use.started_at} className="ml-auto flex-none text-xs text-subtle tabular-nums">
                {formatTime(use.started_at)}
              </time>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
