import { useCheckoutDiff, useMemberDiff } from '@/api/branches'
import { errorText } from '@/api/errorText'
import { CodeBlock } from '@/components/ai-elements/code-block'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Spinner } from '@/components/ui/spinner'
import { useT } from '@/lib/i18n'

export interface DiffDialogProps {
  // Whose changes: a member's worktree, or with checkoutOf the project's
  // checkout itself.
  memberId?: string
  name?: string
  checkoutOf?: string
  onClose: () => void
}

// DiffDialog shows, as one patch, what a member's worktree changed since it
// branched off the main line, committed or not; or what the project's
// checkout changed and did not commit.
export function DiffDialog({ memberId = '', name = '', checkoutOf, onClose }: DiffDialogProps) {
  const t = useT()
  const member = useMemberDiff(memberId, checkoutOf === undefined)
  const checkout = useCheckoutDiff(checkoutOf ?? '', checkoutOf !== undefined)
  const diff = checkoutOf === undefined ? member : checkout
  return (
    <Dialog open onOpenChange={(open) => (open ? undefined : onClose())}>
      <DialogContent aria-describedby={undefined} className="flex max-h-[calc(100dvh-4rem)] flex-col sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>{checkoutOf === undefined ? t('branches.diffTitle', { name }) : t('branches.checkoutDiffTitle')}</DialogTitle>
        </DialogHeader>
        <div className="min-h-0 flex-1 overflow-y-auto">
          {diff.isPending ? (
            <p role="status" className="flex items-center gap-2 text-xs text-subtle">
              <Spinner className="size-3" />
              {t('common.loading')}
            </p>
          ) : diff.isError ? (
            <p className="text-[0.8125rem] text-status-fail">{errorText(diff.error)}</p>
          ) : diff.data.patch === '' ? (
            <p className="py-6 text-center text-[0.8125rem] text-subtle">{t('branches.clean')}</p>
          ) : (
            <>
              {diff.data.cut ? <p className="mb-2 text-xs text-status-wait">{t('branches.diffCut')}</p> : null}
              <CodeBlock code={diff.data.patch} language="diff" />
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
