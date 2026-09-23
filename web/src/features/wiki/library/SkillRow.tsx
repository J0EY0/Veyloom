import { BlocksIcon, ChevronDownIcon } from 'lucide-react'
import { Link } from 'react-router'
import type { WikiTeam } from '@/api/types'
import { librarySpace } from '@/api/wiki'
import { AgentAvatar } from '@/components/shared/agent-avatar'
import { StatusPill } from '@/components/shared/status-pill'
import { AvatarGroup, AvatarGroupCount } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import { Item, ItemActions, ItemContent, ItemDescription, ItemMedia, ItemTitle } from '@/components/ui/item'
import { useT } from '@/lib/i18n'
import { runtimeName } from '@/lib/runtimes'
import { pageHref } from '../links'
import { InstallMenu } from './InstallMenu'
import type { SkillRow as Row } from './model'

// Faces shown before the rest are counted.
const faces = 3

// SkillRow is one skill of the library's list, the way a market lists an
// extension (docs/webui.md 4.10): what it is called and does, whose it is,
// whom it is installed for, and the menu to install it. The title's link
// stretches over the whole row, which opens the skill; the menu sits above
// it, being a button of its own. On a phone the faces and the menu go under
// the text rather than squeeze it.
export function SkillRow({ row, teams }: { row: Row; teams: WikiTeam[] }) {
  const t = useT()
  const team = teams.find((known) => known.slug === row.team)
  const shown = row.installed.slice(0, faces)
  return (
    <Item
      role="listitem"
      className="relative items-start gap-x-3 gap-y-2 rounded-lg px-3 py-3 has-[a:focus-visible]:ring-[3px] has-[a:focus-visible]:ring-ring/50 has-[a:hover]:bg-accent/50 sm:flex-nowrap"
    >
      <ItemMedia variant="icon" className="size-9 rounded-lg border-0 text-muted-foreground">
        <BlocksIcon />
      </ItemMedia>
      <ItemContent className="min-w-0 basis-0 gap-1">
        <ItemTitle className="w-full flex-wrap gap-x-2 gap-y-1">
          <Link to={pageHref(librarySpace, row.path)} className="min-w-0 break-words outline-none after:absolute after:inset-0 after:rounded-lg">
            {row.title}
          </Link>
          {row.onTrial ? (
            <StatusPill tone="wait" dot={false}>
              {t('library.onTrial')}
            </StatusPill>
          ) : null}
          {row.status === 'draft' ? (
            <StatusPill tone="idle" dot={false}>
              {t('wiki.status.draft')}
            </StatusPill>
          ) : null}
          {row.runtimes.length > 0 ? (
            <StatusPill tone="idle" dot={false}>
              {t('library.onlyFor', { runtimes: row.runtimes.map(runtimeName).join('、') })}
            </StatusPill>
          ) : null}
        </ItemTitle>
        {row.description ? <ItemDescription className="text-[0.8125rem] text-pretty">{row.description}</ItemDescription> : null}
        <p className="text-xs text-subtle">{team ? t('library.team', { team: team.name }) : t('skill.unowned')}</p>
      </ItemContent>
      <ItemActions className="w-full gap-3 pl-12 sm:w-auto sm:self-center sm:pl-0">
        {shown.length > 0 ? (
          <AvatarGroup className="-space-x-1.5">
            {shown.map((agent) => (
              <span key={agent.id} className="rounded-full ring-2 ring-background">
                <AgentAvatar look={agent} />
              </span>
            ))}
            {row.installed.length > faces ? (
              <AvatarGroupCount className="size-6 text-[0.6875rem] tabular-nums">+{row.installed.length - faces}</AvatarGroupCount>
            ) : null}
            <span className="sr-only">{t('skill.installedFor', { agents: row.installed.map((agent) => agent.name).join('、') })}</span>
          </AvatarGroup>
        ) : null}
        <InstallMenu name={row.name} skill={row} align="end">
          <Button variant="outline" size="sm" className="relative z-10 font-normal" aria-label={t('library.installName', { name: row.title })}>
            {t('skill.install')}
            <ChevronDownIcon data-icon="inline-end" className="text-subtle" />
          </Button>
        </InstallMenu>
      </ItemActions>
    </Item>
  )
}
