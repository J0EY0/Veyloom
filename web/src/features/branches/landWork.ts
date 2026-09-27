import { useMergeMember } from '@/api/branches'
import { useRunDraft } from '@/api/drafts'

// What putting a member's work on the main line came to: the new commit,
// and why the worktree did not start over after it; or the files that
// conflict, nothing changed.
export interface Landed {
  commit?: string
  conflicts?: string[]
  unsettled?: string
}

interface LandArgs {
  memberId: string
  message: string
  leave: string[]
}

// useLandWork puts a member's work on the main line: straight, or by
// running the draft a member made of it (docs/design.md 5.23.5), which
// keeps what came of it on the draft's card.
export function useLandWork(draftId?: string) {
  const merge = useMergeMember()
  const run = useRunDraft()

  function land(args: LandArgs, handlers: { onSuccess: (landed: Landed) => void; onError: (err: Error) => void }) {
    if (!draftId) {
      merge.mutate(args, handlers)
      return
    }
    run.mutate(
      { id: draftId, message: args.message, leave: args.leave },
      {
        onSuccess: (d) =>
          handlers.onSuccess({ commit: d.result.commit, unsettled: d.result.unsettled, conflicts: d.status === 'conflicted' ? d.result.conflicts : undefined }),
        onError: handlers.onError,
      },
    )
  }

  return { land, isPending: merge.isPending || run.isPending }
}
