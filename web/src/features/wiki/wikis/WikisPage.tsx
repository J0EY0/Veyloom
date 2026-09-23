import { useEffect, useMemo } from 'react'
import { FolderXIcon } from 'lucide-react'
import { Link, Navigate, useParams } from 'react-router'
import { useProjects } from '@/api/projects'
import type { WikiSpace } from '@/api/wiki'
import { Panel } from '@/components/layout/Panel'
import { PanelHeader } from '@/components/layout/PanelHeader'
import { Button } from '@/components/ui/button'
import { Empty, EmptyContent, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { useT } from '@/lib/i18n'
import { useDocumentTitle } from '@/lib/useDocumentTitle'
import { wikisHref } from '../links'
import { WikiView } from '../WikiView'
import { AllWikis } from './AllWikis'
import { lastWiki, rememberWiki } from './lastWiki'
import { ProjectPicker } from './ProjectPicker'

// WikisPage is the Wiki page of the sidebar (docs/design.md 5.18, webui.md
// 4.15): the projects' wikis beside their chats' Wiki tabs. With no project
// in the address it is every project's; with one, that project's wiki as
// its tab reads it, at addresses of this page. It opens again at the
// project last chosen.
export function WikisPage() {
  const t = useT()
  useDocumentTitle(t('wikis.title'))
  const params = useParams()
  const projectId = params.projectId ?? ''
  const rest = params['*'] ?? ''
  const projects = useProjects()
  const project = projects.data?.find((known) => known.id === projectId)
  const space = useMemo<WikiSpace | undefined>(
    () => (project ? { kind: 'project', projectId: project.id, roomId: project.main_room_id, standalone: true } : undefined),
    [project],
  )
  useEffect(() => {
    if (projectId) rememberWiki(projectId)
  }, [projectId])

  // Every project's, unless a project was chosen last time and is still
  // there: wait for the list before deciding.
  const last = projectId ? '' : lastWiki()
  if (last && projects.data?.some((known) => known.id === last)) return <Navigate to={wikisHref(last)} replace />
  const deciding = last !== '' && projects.isPending

  return (
    <Panel>
      <PanelHeader title={t('wikis.title')} actions={<ProjectPicker projects={projects.data ?? []} current={projectId} />} />
      {deciding ? null : !projectId ? (
        <AllWikis />
      ) : space ? (
        <WikiView key={projectId} space={space} rest={rest} />
      ) : projects.isPending ? null : (
        <Empty className="h-full">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <FolderXIcon />
            </EmptyMedia>
            <EmptyTitle>{t('wikis.missing')}</EmptyTitle>
          </EmptyHeader>
          <EmptyContent>
            <Button variant="outline" size="sm" asChild>
              <Link to={wikisHref()} onClick={() => rememberWiki('')}>
                {t('wikis.all')}
              </Link>
            </Button>
          </EmptyContent>
        </Empty>
      )}
    </Panel>
  )
}
