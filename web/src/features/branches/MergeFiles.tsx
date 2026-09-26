import { useId } from 'react'
import type { WorktreeChange } from '@/api/types'
import { Badge } from '@/components/ui/badge'
import { Checkbox } from '@/components/ui/checkbox'
import { useT } from '@/lib/i18n'

export interface MergeFilesProps {
  // The member whose work it is.
  name: string
  files: WorktreeChange[]
  // The new files left out of the merge.
  leave: ReadonlySet<string>
  onLeave: (path: string, left: boolean) => void
}

// MergeFiles lists what a merge takes (docs/design.md 5.21): the files the
// member changed, then those it made and never committed, each with a box.
// A file whose box is cleared stays in the member's worktree, off the main
// line: what a build or a run wrote, most likely, which is why a binary
// one starts cleared.
export function MergeFiles({ name, files, leave, onLeave }: MergeFilesProps) {
  const t = useT()
  const id = useId()
  const changed = files.filter((file) => !file.new)
  const fresh = files.filter((file) => file.new)
  return (
    <div className="flex max-h-52 flex-col gap-3 overflow-y-auto">
      {changed.length > 0 ? (
        <ul className="flex flex-col gap-0.5 font-mono text-[0.78125rem] text-muted-foreground" translate="no">
          {changed.map((file) => (
            <li key={file.path} className="flex gap-2 break-all">
              <span className="w-3 flex-none text-subtle">{file.status}</span>
              <span className="min-w-0">{file.path}</span>
            </li>
          ))}
        </ul>
      ) : null}
      {fresh.length > 0 ? (
        <section aria-label={t('branches.newFiles')} className="flex flex-col gap-1.5">
          <p className="text-xs text-subtle">{t('branches.newFilesHint', { name })}</p>
          <ul className="flex flex-col gap-1">
            {fresh.map((file) => {
              const box = `${id}-${file.path}`
              return (
                <li key={file.path} className="flex items-center gap-2.5">
                  <Checkbox id={box} checked={!leave.has(file.path)} onCheckedChange={(on) => onLeave(file.path, on !== true)} />
                  <label htmlFor={box} className="min-w-0 font-mono text-[0.78125rem] break-all text-muted-foreground" translate="no">
                    {file.path}
                  </label>
                  {file.binary ? (
                    <Badge variant="outline" className="h-4.5 flex-none rounded-[5px] px-1.5 text-[0.6875rem] font-normal text-subtle">
                      {t('branches.binary')}
                    </Badge>
                  ) : null}
                </li>
              )
            })}
          </ul>
        </section>
      ) : null}
    </div>
  )
}

// leftOutAtFirst are the new files a merge leaves out unless a person says
// otherwise: the binary ones.
export function leftOutAtFirst(files: WorktreeChange[]): Set<string> {
  return new Set(files.filter((file) => file.new && file.binary).map((file) => file.path))
}
