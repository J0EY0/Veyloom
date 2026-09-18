import { useId, useState, type FormEvent } from 'react'
import { useAgents, useCreateMember } from '@/api/agents'
import { useProject } from '@/api/projects'
import { useRoom } from '@/api/rooms'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { runtimeName } from '@/lib/runtimes'
import { inheritPreset, presetFromForm, presetLabel, presets } from './presets'
import { useT } from '@/lib/i18n'

export interface AddMemberDialogProps {
  roomId: string
  roomName: string
  open: boolean
  onClose: () => void
}

// Adds a member to the room: an agent, which runs on the machine
// it is set up on, with the name, repository and permission a person may
// override. Each agent in the list says which machine that is. The
// repository starts as the project's checkout, where its members work.
export function AddMemberDialog({ roomId, roomName, open, onClose }: AddMemberDialogProps) {
  const agents = useAgents()
  const create = useCreateMember(roomId)
  const [agentId, setAgentId] = useState('')
  const [error, setError] = useState<string>()
  const id = useId()
  const t = useT()
  const agent = agents.data?.find((candidate) => candidate.id === agentId)
  const project = useProject(useRoom(roomId).data?.project_id ?? '')

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    const picked = String(data.get('agent_id') ?? '')
    if (picked === '') {
      setError(t('member.agentRequired'))
      return
    }
    setError(undefined)
    create.mutate(
      {
        agent_id: picked,
        display_name: String(data.get('display_name') ?? '').trim(),
        repo_path: String(data.get('repo_path') ?? '').trim(),
        model: String(data.get('model') ?? '').trim(),
        permission_preset: presetFromForm(data.get('permission_preset')),
      },
      {
        onSuccess: () => {
          setAgentId('')
          onClose()
        },
        onError: (err) => setError(err.message),
      },
    )
  }

  return (
    <Dialog open={open} onOpenChange={(next) => (next ? undefined : onClose())}>
      <DialogContent className="sm:max-w-md" aria-describedby={undefined}>
        <form onSubmit={onSubmit}>
          <DialogHeader>
            <DialogTitle>{t('member.addTitle', { room: roomName })}</DialogTitle>
          </DialogHeader>
          <FieldGroup className="my-4 gap-4">
            <Field>
              <FieldLabel htmlFor={`${id}-agent`}>{t('member.agent')}</FieldLabel>
              <Select name="agent_id" value={agentId} onValueChange={setAgentId}>
                <SelectTrigger id={`${id}-agent`} className="w-full">
                  <SelectValue placeholder={agents.isPending ? t('common.loading') : t('member.pickAgent')} />
                </SelectTrigger>
                <SelectContent>
                  {agents.data?.map((candidate) => (
                    <SelectItem key={candidate.id} value={candidate.id}>
                      {candidate.name}
                      <span className="text-subtle">
                        {' · '}
                        {candidate.machine_name} · {runtimeName(candidate.runtime)}
                        {candidate.model ? ` · ${candidate.model}` : ''} · {presetLabel(candidate.permission_preset)}
                      </span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel htmlFor={`${id}-name`}>{t('member.displayName')}</FieldLabel>
              <Input id={`${id}-name`} name="display_name" autoComplete="off" spellCheck={false} placeholder={agent?.name ?? t('member.defaultName')} />
            </Field>
            <Field>
              <FieldLabel htmlFor={`${id}-repo`}>{t('member.repoPath')}</FieldLabel>
              <Input
                key={project?.repo_path ?? ''}
                id={`${id}-repo`}
                name="repo_path"
                defaultValue={project?.repo_path ?? ''}
                autoComplete="off"
                spellCheck={false}
                placeholder="/Users/me/project"
              />
            </Field>
            <div className="grid grid-cols-2 gap-4">
              <Field>
                <FieldLabel htmlFor={`${id}-permission`}>{t('member.permission')}</FieldLabel>
                <Select name="permission_preset" defaultValue={inheritPreset}>
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
                <Input id={`${id}-model`} name="model" autoComplete="off" spellCheck={false} placeholder={t('member.modelPlaceholder')} />
              </Field>
            </div>
            {error ? <FieldError>{error}</FieldError> : null}
          </FieldGroup>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={create.isPending} aria-busy={create.isPending}>
              {create.isPending ? t('member.adding') : t('member.addTo', { room: roomName })}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
