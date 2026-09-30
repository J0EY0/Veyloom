import { useState } from 'react'
import { toast } from 'sonner'
import { errorText } from '@/api/errorText'
import type { LocalSkill } from '@/api/types'
import { useUpdateSkill } from '@/api/wiki'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { useT } from '@/lib/i18n'

// UpdateFromHere takes a skill of this machine into the library again,
// over the library's copy that came from this folder (docs/design.md
// 5.15). When the copy was changed since, by agents improving it say, it
// asks first: those changes are replaced, though the library's history
// can undo it.
export function UpdateFromHere({ skill }: { skill: LocalSkill }) {
  const t = useT()
  const update = useUpdateSkill()
  const [asking, setAsking] = useState(false)
  const edits = skill.edits_since ?? 0

  function run() {
    update.mutate(skill.folder, {
      onSuccess: () => {
        toast.success(t('library.import.updated', { name: skill.name }))
        setAsking(false)
      },
      onError: (err) => {
        toast.error(t('library.import.updateFailed', { error: errorText(err) }))
        setAsking(false)
      },
    })
  }

  return (
    <>
      <Button
        type="button"
        variant="outline"
        size="sm"
        className="h-7 shrink-0 px-2.5 text-xs"
        disabled={update.isPending}
        aria-label={t('library.import.updateName', { name: skill.name })}
        onClick={() => (edits > 0 ? setAsking(true) : run())}
      >
        {update.isPending ? t('library.import.updating') : t('library.import.update')}
      </Button>
      {asking ? (
        <AlertDialog open onOpenChange={(next) => (next ? undefined : setAsking(false))}>
          <AlertDialogContent size="sm">
            <AlertDialogHeader>
              <AlertDialogTitle className="text-base">{t('library.import.updateTitle', { name: skill.name })}</AlertDialogTitle>
              <AlertDialogDescription>{t('library.import.updateBody', { n: edits })}</AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t('common.cancel')}</AlertDialogCancel>
              <AlertDialogAction
                disabled={update.isPending}
                onClick={(event) => {
                  event.preventDefault()
                  run()
                }}
              >
                {update.isPending ? t('library.import.updating') : t('library.import.update')}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      ) : null}
    </>
  )
}
