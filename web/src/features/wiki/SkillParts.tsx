import { ActivityIcon, BlocksIcon, ChevronDownIcon, FlaskConicalIcon, HistoryIcon, ShieldCheckIcon, UsersIcon } from 'lucide-react'
import { toast } from 'sonner'
import { useProjects } from '@/api/projects'
import type { WikiPage, WikiTeam } from '@/api/types'
import { skillName, useChangeWikiPage, librarySpace } from '@/api/wiki'
import { Chip, ChipText } from '@/components/shared/chip'
import { ProjectMark } from '@/components/shared/project-mark'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { formatTime } from '@/lib/format'
import { useT } from '@/lib/i18n'
import type { OpenTopic } from './CommitRow'
import { Fact, Facts, TrustPill } from './Facts'
import { actorName, producerName } from './names'
import { PageHistory } from './PageParts'
import { SkillInstalls, SkillUses } from './SkillLists'
import { SkillTrialEnded } from './SkillTrial'
import { errorText } from '@/api/errorText'

// What only a skill of the library has (docs/design.md 5.10, 5.15), all in
// one block above its text (docs/webui.md 4.10): the team that owns it,
// which a person may hand it on from; the agents it is installed for; how
// far to trust it, and how its last trial ended; the turns that used it,
// which that team looks back on; and what changed it. A line sets them
// apart from the text (WikiPageView); under the text are only the skill and
// its files.

export interface SkillFactsProps {
  page: WikiPage
  teams: WikiTeam[]
  canUndo: boolean
  onOpenThread: OpenTopic
}

export function SkillFacts({ page, teams, canUndo, onOpenThread }: SkillFactsProps) {
  const t = useT()
  return (
    <Facts className="mt-4">
      <Fact icon={UsersIcon} label={t('skill.facts.team')}>
        <SkillTeam page={page} teams={teams} />
      </Fact>
      <Fact icon={BlocksIcon} label={t('skill.facts.installed')}>
        <SkillInstalls page={page} />
      </Fact>
      <Fact icon={ShieldCheckIcon} label={t('wiki.details.trust')}>
        <SkillTrust page={page} />
      </Fact>
      {page.trial && page.trial.status !== 'open' ? (
        <Fact icon={FlaskConicalIcon} label={t('skill.trial.title')}>
          <SkillTrialEnded trial={page.trial} />
        </Fact>
      ) : null}
      <Fact icon={ActivityIcon} label={t('skill.facts.used')}>
        <SkillUses name={skillName(page.path)} />
      </Fact>
      <Fact icon={HistoryIcon} label={t('wiki.page.history')}>
        <PageHistory page={page} space={librarySpace} canUndo={canUndo} teams={teams} onOpenThread={onOpenThread} compact />
      </Fact>
    </Facts>
  )
}

// SkillTeam names the team that owns a skill and hands it to another.
function SkillTeam({ page, teams }: { page: WikiPage; teams: WikiTeam[] }) {
  const t = useT()
  const projects = useProjects()
  const change = useChangeWikiPage(librarySpace)
  const owner = teams.find((team) => team.slug === page.team)
  const others = (projects.data ?? []).filter((project) => project.id !== owner?.project_id)

  return (
    <>
      {owner ? (
        <Chip>
          <ProjectMark name={owner.name} />
          <ChipText>{owner.name}</ChipText>
        </Chip>
      ) : (
        <span className="text-status-wait">{page.team ? t('skill.teamGone', { team: page.team }) : t('skill.unowned')}</span>
      )}
      {others.length > 0 ? (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="xs" className="h-6 gap-1 px-1.5 text-muted-foreground" disabled={change.isPending}>
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
    </>
  )
}

// SkillTrust says how far to trust a skill: its tier, then who wrote it
// and the person who confirmed it, if one did.
function SkillTrust({ page }: { page: WikiPage }) {
  const t = useT()
  const person = [...page.verified].reverse().find((stamp) => stamp.by.startsWith('human:'))
  const parts: string[] = []
  if (page.generated_by && page.generated_at) {
    parts.push(t('wiki.page.writtenBy', { who: producerName(page.generated_by), when: formatTime(page.generated_at) }))
  }
  if (person) parts.push(t('wiki.page.confirmedBy', { who: actorName(person.by), when: formatTime(person.at) }))
  return (
    <>
      <TrustPill tier={page.tier} />
      {parts.length > 0 ? <span className="text-xs text-subtle">{parts.join(' · ')}</span> : null}
    </>
  )
}
