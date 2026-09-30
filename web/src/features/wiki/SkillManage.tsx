import { useState, type ReactNode } from 'react'
import { ArchiveIcon, ArchiveRestoreIcon, Trash2Icon } from 'lucide-react'
import { useNavigate } from 'react-router'
import { toast } from 'sonner'
import { errorText } from '@/api/errorText'
import type { WikiPage } from '@/api/types'
import { librarySpace, skillName, useDeleteSkill, useRetireSkill } from '@/api/wiki'
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
import { DropdownMenuItem, DropdownMenuSeparator } from '@/components/ui/dropdown-menu'
import { useT } from '@/lib/i18n'
import { pageHref } from './links'

// What a person does with a skill of the library as a whole (docs/design.md
// 5.15): take it out of use and put it back, or remove it. useSkillManage
// gives the items of the skill page's "…" menu, the removal apart to close
// the menu, and the dialog that asks before a removal, which stays up once
// the menu has closed.
export function useSkillManage(page: WikiPage): { items: ReactNode; removal: ReactNode; dialog: ReactNode } {
  const t = useT()
  const retire = useRetireSkill()
  const [asking, setAsking] = useState(false)
  const name = skillName(page.path)
  if (!name || page.type !== 'Skill') return { items: null, removal: null, dialog: null }
  const retired = page.status === 'deprecated'

  const items = (
    <>
      <DropdownMenuItem
        disabled={retire.isPending}
        onSelect={() =>
          retire.mutate(
            { name, retired: !retired },
            {
              onSuccess: () => toast.success(t(retired ? 'skill.restoredToast' : 'skill.retiredToast', { name })),
              onError: (err) => toast.error(t('wiki.page.changeFailed', { error: errorText(err) })),
            },
          )
        }
      >
        {retired ? <ArchiveRestoreIcon /> : <ArchiveIcon />}
        {retired ? t('skill.restore') : t('skill.retire')}
      </DropdownMenuItem>
    </>
  )
  const removal = (
    <>
      <DropdownMenuSeparator />
      <DropdownMenuItem variant="destructive" onSelect={() => setAsking(true)}>
        <Trash2Icon />
        {t('skill.delete')}
      </DropdownMenuItem>
    </>
  )
  const dialog = asking ? <DeleteSkillDialog name={name} installed={page.installed?.length ?? 0} onClose={() => setAsking(false)} /> : null
  return { items, removal, dialog }
}

// DeleteSkillDialog asks before a skill leaves the library, saying whom it
// is taken off; it stays up until the hub answers, so a refusal is read
// here.
function DeleteSkillDialog({ name, installed, onClose }: { name: string; installed: number; onClose: () => void }) {
  const t = useT()
  const navigate = useNavigate()
  const remove = useDeleteSkill()
  return (
    <AlertDialog open onOpenChange={(next) => (next ? undefined : onClose())}>
      <AlertDialogContent size="sm">
        <AlertDialogHeader>
          <AlertDialogTitle className="text-base">{t('skill.deleteTitle', { name })}</AlertDialogTitle>
          <AlertDialogDescription>{installed > 0 ? t('skill.deleteBody', { n: installed }) : t('skill.deleteBodyNone')}</AlertDialogDescription>
        </AlertDialogHeader>
        {remove.error ? (
          <p role="alert" className="text-center text-[0.8125rem] leading-relaxed text-status-fail">
            {errorText(remove.error)}
          </p>
        ) : null}
        <AlertDialogFooter>
          <AlertDialogCancel>{t('common.cancel')}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={remove.isPending}
            onClick={(event) => {
              // The action would close the dialog at once; wait for the hub.
              event.preventDefault()
              remove.mutate(name, {
                onSuccess: () => {
                  toast.success(t('skill.deletedToast', { name }))
                  onClose()
                  void navigate(pageHref(librarySpace))
                },
              })
            }}
          >
            {remove.isPending ? t('common.deleting') : t('common.delete')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
