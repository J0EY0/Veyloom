import { useId, useState, type FormEvent } from 'react'
import { useMemberSession, useUpdateMember } from '@/api/agents'
import type { Member } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { inheritPreset, presetFromForm, presetLabel, presets } from './presets'
import { formatAgo } from '@/lib/format'
import { useT } from '@/lib/i18n'

export interface EditMemberDialogProps {
  roomId: string
  member: Member
  onClose: () => void
}

// Edits what may change after a member is added: name, repository,
// permission and model overrides.
export function EditMemberDialog({ roomId, member, onClose }: EditMemberDialogProps) {
  const update = useUpdateMember(roomId)
  const session = useMemberSession(member.id)
  const [error, setError] = useState<string>()
  const id = useId()
  const t = useT()

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    const name = String(data.get('display_name') ?? '').trim()
    if (name === '') {
      setError(t('member.nameRequired'))
      return
    }
    const patch = {
      display_name: name,
      repo_path: String(data.get('repo_path') ?? '').trim(),
      permission_preset: presetFromForm(data.get('permission_preset')),
      model: String(data.get('model') ?? '').trim(),
    }
    setError(undefined)
    update.mutate({ id: member.id, patch }, { onSuccess: onClose, onError: (err) => setError(err.message) })
  }

  return (
    <Dialog open onOpenChange={(next) => (next ? undefined : onClose())}>
      <DialogContent className="sm:max-w-md" aria-describedby={undefined}>
        <form onSubmit={onSubmit}>
          <DialogHeader>
            <DialogTitle>{t('member.editTitle', { name: member.display_name })}</DialogTitle>
          </DialogHeader>
          <FieldGroup className="my-4 gap-4">
            <Field data-invalid={error ? true : undefined}>
              <FieldLabel htmlFor={`${id}-name`}>{t('member.displayName')}</FieldLabel>
              <Input
                id={`${id}-name`}
                name="display_name"
                defaultValue={member.display_name}
                autoComplete="off"
                spellCheck={false}
                aria-invalid={error ? true : undefined}
              />
              {error ? <FieldError>{error}</FieldError> : null}
            </Field>
            <Field>
              <FieldLabel htmlFor={`${id}-repo`}>{t('member.repoPath')}</FieldLabel>
              <Input id={`${id}-repo`} name="repo_path" defaultValue={member.repo_path} autoComplete="off" spellCheck={false} />
              {/* Moving a member starts its session over: a runtime's session
                  belongs to the directory it was opened in. */}
              {session.data?.session ? <FieldDescription>{t('member.repoPathHint')}</FieldDescription> : null}
            </Field>
            <div className="grid grid-cols-2 gap-4">
              <Field>
                <FieldLabel htmlFor={`${id}-permission`}>{t('member.permission')}</FieldLabel>
                <Select name="permission_preset" defaultValue={member.permission_preset || inheritPreset}>
                  <SelectTrigger id={`${id}-permission`} className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={inheritPreset}>{t('member.followAgent')}</SelectItem>
                    {presets.map((p) => (
                      <SelectItem key={p} value={p}>
                        {presetLabel(p)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <Field>
                <FieldLabel htmlFor={`${id}-model`}>{t('member.model')}</FieldLabel>
                <Input
                  id={`${id}-model`}
                  name="model"
                  defaultValue={member.model}
                  autoComplete="off"
                  spellCheck={false}
                  placeholder={t('member.modelPlaceholder')}
                />
              </Field>
            </div>
          </FieldGroup>
          {/* The member's conversation with its runtime. It looks after
              itself; this only says how long it has been going. */}
          {session.data ? (
            <p className="mb-4 text-xs text-subtle">
              {session.data.session
                ? t('member.session', {
                    since: formatAgo(session.data.session.started_at),
                    turns: session.data.turns,
                    compactions: session.data.session.compactions,
                  })
                : t('member.sessionNone')}
            </p>
          ) : null}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={update.isPending} aria-busy={update.isPending}>
              {update.isPending ? t('common.saving') : t('common.save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
