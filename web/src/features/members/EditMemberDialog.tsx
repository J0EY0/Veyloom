import { useId, useState, type FormEvent } from 'react'
import { useAgents, useMemberSession, useUpdateMember } from '@/api/agents'
import type { Member } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { MemberRules } from './MemberRules'
import { PresetField } from './PresetField'
import { presetFromForm } from './presets'
import { formatAgo } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { errorText } from '@/api/errorText'

export interface EditMemberDialogProps {
  roomId: string
  member: Member
  onClose: () => void
}

// Edits what may change after a member is added: name, repository,
// permission and model overrides; and lists the commands people allowed it
// always, each of which can be taken back (docs/design.md 4.6).
export function EditMemberDialog({ roomId, member, onClose }: EditMemberDialogProps) {
  const update = useUpdateMember(roomId)
  const session = useMemberSession(member.id)
  const agent = useAgents().data?.find((a) => a.id === member.agent_id)
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
    update.mutate({ id: member.id, patch }, { onSuccess: onClose, onError: (err) => setError(errorText(err)) })
  }

  return (
    <Dialog open onOpenChange={(next) => (next ? undefined : onClose())}>
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-md" aria-describedby={undefined}>
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
            <PresetField idPrefix={`${id}-permission`} defaultValue={member.permission_preset} agentPreset={agent?.permission_preset} />
            <MemberRules memberId={member.id} runtime={agent?.runtime} />
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
