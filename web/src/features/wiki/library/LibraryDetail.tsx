import { Link } from 'react-router'
import type { WikiCatalog, WikiPageInfo } from '@/api/types'
import { librarySpace, skillName, skillPath } from '@/api/wiki'
import { useT, type t as translate } from '@/lib/i18n'
import type { OpenTopic } from '../CommitRow'
import { pageHref } from '../links'
import type { Back } from '../WikiOverview'
import { WikiPageView } from '../WikiPageView'
import { backOf, skillFiles } from './model'

// LibraryDetail is one page of the library shown alone (docs/webui.md
// 4.10): a skill, one of the files it came with, or a pattern, leading
// back to the list it is on, or a file to its skill. A skill also lists
// the files it came with.
export function LibraryDetail({ path, catalog, onOpenThread }: { path: string; catalog?: WikiCatalog; onOpenThread: OpenTopic }) {
  const t = useT()
  const name = skillName(path)
  const files = name ? skillFiles(catalog, name) : []
  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <WikiPageView
        key={path}
        space={librarySpace}
        path={path}
        history={catalog?.history ?? true}
        teams={catalog?.teams}
        onOpenThread={onOpenThread}
        back={backTo(path, catalog, t)}
        afterBody={files.length > 0 ? <SkillFiles name={name} files={files} /> : null}
      />
    </div>
  )
}

function backTo(path: string, catalog: WikiCatalog | undefined, t: typeof translate): Back {
  const back = backOf(path)
  if (back.to === 'patterns') return { href: pageHref(librarySpace, '/patterns'), label: t('library.tab.patterns') }
  if (back.to === 'skill') {
    const skill = catalog?.pages.find((page) => page.path === skillPath(back.name))
    return { href: pageHref(librarySpace, skillPath(back.name)), label: skill?.title ?? back.name }
  }
  return { href: pageHref(librarySpace), label: t('library.title') }
}

// SkillFiles lists the other pages of a skill's folder, the references it
// came with, by title and where in the folder each is.
function SkillFiles({ name, files }: { name: string; files: WikiPageInfo[] }) {
  const t = useT()
  const folder = `/skills/${name}/`
  return (
    <section aria-labelledby="skill-files" className="mt-8">
      <h2 id="skill-files" className="mb-1.5 text-xs font-medium text-subtle">
        {t('library.files')}
      </h2>
      <ul className="grid gap-1 text-[0.8125rem]">
        {files.map((file) => (
          <li key={file.path} className="flex min-w-0 flex-wrap items-baseline gap-x-2">
            <Link to={pageHref(librarySpace, file.path)} className="break-words underline-offset-3 hover:underline">
              {file.title}
            </Link>
            <span className="min-w-0 font-mono text-xs break-all text-subtle" translate="no">
              {file.path.slice(folder.length)}
            </span>
          </li>
        ))}
      </ul>
    </section>
  )
}
