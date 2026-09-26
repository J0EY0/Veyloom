import { useId, useState, type FormEvent } from 'react'
import { toast } from 'sonner'
import { errorText } from '@/api/errorText'
import { useUpdateProject } from '@/api/projects'
import type { Project } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { useT } from '@/lib/i18n'

export interface StepsDialogProps {
  project: Project
  onClose: () => void
}

// StepsDialog lets a person write down how a new worktree is got ready
// (docs/design.md 5.21), in place of the leader: what to copy from the
// checkout, one a line, and the command to run. Saving sets the project
// up; members waiting for that go on.
export function StepsDialog({ project, onClose }: StepsDialogProps) {
  const t = useT()
  const id = useId()
  const update = useUpdateProject(project.id)
  const [error, setError] = useState<string>()
  const steps = project.workspace_pending ?? { copy: project.workspace_copy ?? [], run: project.workspace_run ?? '' }

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    const copy = String(data.get('copy') ?? '')
      .split('\n')
      .map((line) => line.trim())
      .filter((line) => line !== '')
    const run = String(data.get('run') ?? '').trim()
    setError(undefined)
    update.mutate(
      { workspace_steps: { copy, run } },
      {
        onSuccess: () => {
          toast.success(t('branches.steps.saved'))
          onClose()
        },
        onError: (err) => setError(errorText(err)),
      },
    )
  }

  return (
    <Dialog open onOpenChange={(open) => (open ? undefined : onClose())}>
      <DialogContent aria-describedby={undefined}>
        <form onSubmit={onSubmit}>
          <DialogHeader>
            <DialogTitle>{t('branches.steps.title')}</DialogTitle>
          </DialogHeader>
          <FieldGroup className="my-4 gap-4">
            <Field>
              <FieldLabel htmlFor={`${id}-copy`}>{t('branches.steps.copy')}</FieldLabel>
              <Textarea
                id={`${id}-copy`}
                name="copy"
                rows={4}
                defaultValue={steps.copy.join('\n')}
                placeholder={'.env\ndocs/'}
                spellCheck={false}
                className="font-mono text-[0.8125rem]"
              />
            </Field>
            <Field>
              <FieldLabel htmlFor={`${id}-run`}>{t('branches.steps.run')}</FieldLabel>
              <Input
                id={`${id}-run`}
                name="run"
                defaultValue={steps.run}
                placeholder="cd web && npm ci"
                spellCheck={false}
                autoComplete="off"
                className="font-mono"
              />
            </Field>
            {error ? <FieldError className="wrap-anywhere">{error}</FieldError> : null}
          </FieldGroup>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={update.isPending}>
              {update.isPending ? t('common.saving') : t('common.save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
