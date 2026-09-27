import { CircleCheckIcon, GitBranchIcon, TriangleAlertIcon } from 'lucide-react'
import type { MemberBranch } from '@/api/types'
import { useT } from '@/lib/i18n'
import type { MemberOverlap } from './overlaps'

// overlapFilesShown caps the files an overlap line names.
const overlapFilesShown = 3

export interface BranchLinesProps {
  branch: MemberBranch
  // The files it changed that other members changed too.
  overlaps: MemberOverlap[]
  // The members whose work its branch has in full, by name.
  holds: string[]
}

// BranchLines is a member's worktree against the main line, under its name
// in the chat's info (docs/design.md 5.21): the branch, what it changed and
// how far behind it is; then, in orange, what needs a person: files another
// member changed too, a merge stopped on conflicts.
export function BranchLines({ branch, overlaps, holds }: BranchLinesProps) {
  const t = useT()
  const st = branch.status
  const files = st?.files ?? []
  const added = files.reduce((sum, file) => sum + file.added, 0)
  const deleted = files.reduce((sum, file) => sum + file.deleted, 0)

  let state
  if (!branch.dir) state = t('branches.noWorktree')
  else if (branch.error) state = <span className="text-status-fail">{branch.error}</span>
  else if (!branch.prepared) state = t('branches.notReady')
  else if (st) {
    const parts = [
      files.length > 0
        ? t('branches.files', { n: files.length }) + (st.uncommitted > 0 ? t('branches.filesUncommitted', { n: st.uncommitted }) : '')
        : t('branches.clean'),
    ]
    if (st.behind > 0) parts.push(t('branches.behind', { n: st.behind }))
    state = parts.join(' · ')
  }

  return (
    <>
      <span className="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-0.5 text-[0.71875rem] text-subtle">
        {branch.branch ? (
          <span className="flex min-w-0 items-center gap-1 font-mono text-[0.6875rem]" translate="no">
            <GitBranchIcon aria-hidden="true" className="size-3 flex-none" />
            <span className="truncate">{branch.branch}</span>
          </span>
        ) : null}
        {added + deleted > 0 ? (
          <span className="font-mono text-[0.6875rem] tabular-nums" translate="no">
            <span className="text-status-ok">+{added}</span> <span className="text-status-fail">−{deleted}</span>
          </span>
        ) : null}
        <span>{state}</span>
      </span>
      {st?.merging ? (
        <Warning>
          {t('branches.stuckMerge')}
          {st.conflicts?.length ? (
            <span className="font-mono break-all" translate="no">
              {' '}
              {st.conflicts.join('、')}
            </span>
          ) : null}
        </Warning>
      ) : null}
      {overlaps.map((o) => {
        const shown = o.files.slice(0, overlapFilesShown).join('、')
        const named = o.files.length > overlapFilesShown ? t('branches.overlapMore', { files: shown, n: o.files.length }) : shown
        return (
          <Warning key={o.names.join()}>
            {t('branches.overlapWith', { names: o.names.join('、') })}{' '}
            <span className="font-mono break-all" translate="no">
              {named}
            </span>
          </Warning>
        )
      })}
      {holds.length > 0 ? (
        <span className="flex items-start gap-1.5 text-[0.71875rem] text-muted-foreground">
          <CircleCheckIcon aria-hidden="true" className="mt-px size-3 flex-none text-status-ok" />
          <span>{t('branches.holds', { names: holds.join(t('common.listSeparator')) })}</span>
        </span>
      ) : null}
    </>
  )
}

function Warning({ children }: { children: React.ReactNode }) {
  return (
    <span className="flex items-start gap-1.5 text-[0.71875rem] text-status-wait">
      <TriangleAlertIcon aria-hidden="true" className="mt-px size-3 flex-none" />
      <span className="min-w-0">{children}</span>
    </span>
  )
}
