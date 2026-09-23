import { Link } from 'react-router'
import { useMemoryPrefs } from '@/api/settings'
import type { MemorySpace } from '@/api/wiki'
import { useT } from '@/lib/i18n'
import type { OpenTopic } from './CommitRow'
import { MemoryEditor } from './Memory'
import { BackToPages } from './WikiOverview'

// The project memory in the Wiki tab (docs/design.md 5.16): how to work in
// this project, which every turn of its members carries whole. Turned off
// in Settings (5.19), it is still kept here, and says it is off.
export function ProjectMemory({ space, onOpenThread }: { space: MemorySpace; onOpenThread: OpenTopic }) {
  const t = useT()
  const prefs = useMemoryPrefs()
  const off = prefs.data !== undefined && !(prefs.data.enabled && prefs.data.project)
  return (
    <div className="mx-auto flex w-full max-w-[46rem] flex-col gap-5 px-5 pt-5 pb-12 md:px-10">
      <header className="flex flex-col gap-1">
        <BackToPages space={space} />
        <h1 className="text-xl font-semibold tracking-[-0.01em]">{t('wiki.memory')}</h1>
        {off ? (
          <p className="text-[0.8125rem] text-status-wait">
            {t('memory.projectOff')}{' '}
            <Link to="/settings/general" className="underline underline-offset-3">
              {t('memory.toSettings')}
            </Link>
          </p>
        ) : (
          <p className="text-[0.8125rem] text-muted-foreground">{t('memory.projectAbout')}</p>
        )}
      </header>
      <MemoryEditor space={space} onOpenThread={onOpenThread} />
    </div>
  )
}
