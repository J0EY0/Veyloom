import { useId, useRef, useState, type FormEvent } from 'react'
import { useUpdateProject } from '@/api/projects'
import type { Project, UpkeepTrigger } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { useT } from '@/lib/i18n'
import { MaintainerFields, noMaintainer } from './MaintainerFields'
import { errorText } from '@/api/errorText'

export interface EditProjectDialogProps {
  project: Project
  // Asks for the name alone, as the sidebar's rename does.
  rename?: boolean
  onClose: () => void
}

// Renames a project, moves its checkout or says what it is. Members work
// under the checkout, so moving it moves them along (the hub does that,
// 2026-09-17). The description opens every agent's brief (2026-09-18): what
// the project is for, its goals, its stack, written once by a person. The
// switch lets the members write to the project wiki (on by default,
// docs/design.md 6.5); one member may be chosen to keep it (5.12).
export function EditProjectDialog({ project, rename = false, onClose }: EditProjectDialogProps) {
  const update = useUpdateProject(project.id)
  const nameRef = useRef<HTMLInputElement>(null)
  const [nameError, setNameError] = useState<string>()
  const [error, setError] = useState<string>()
  const [maintainer, setMaintainer] = useState(project.wiki_maintainer_member_id || noMaintainer)
  const [trigger, setTrigger] = useState<UpkeepTrigger>(project.wiki_maintainer_trigger ?? 'daily')
  const id = useId()
  const t = useT()

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    const name = String(data.get('name') ?? '').trim()
    if (name === '') {
      setNameError(t('project.nameRequired'))
      nameRef.current?.focus()
      return
    }
    setNameError(undefined)
    setError(undefined)
    const req = rename
      ? { name }
      : {
          name,
          repo_path: String(data.get('repo_path') ?? '').trim(),
          description: String(data.get('description') ?? '').trim(),
          ...maintainerChange(project, maintainer === noMaintainer ? '' : maintainer, trigger),
          ...mountsChange(project, String(data.get('external_bundles') ?? '')),
        }
    update.mutate(req, { onSuccess: onClose, onError: (err) => setError(errorText(err)) })
  }

  return (
    <Dialog open onOpenChange={(next) => (next ? undefined : onClose())}>
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-md" aria-describedby={undefined}>
        <form onSubmit={onSubmit}>
          <DialogHeader>
            <DialogTitle>{rename ? t('project.renameTitle') : t('project.edit')}</DialogTitle>
          </DialogHeader>
          <FieldGroup className="my-4 gap-4">
            <Field data-invalid={nameError ? true : undefined}>
              <FieldLabel htmlFor={`${id}-name`}>{t('project.name')}</FieldLabel>
              <Input
                id={`${id}-name`}
                ref={nameRef}
                name="name"
                defaultValue={project.name}
                autoComplete="off"
                spellCheck={false}
                aria-invalid={nameError ? true : undefined}
              />
              {nameError ? <FieldError>{nameError}</FieldError> : null}
            </Field>
            {rename ? null : (
              <Field>
                <FieldLabel htmlFor={`${id}-path`}>{t('project.repoPath')}</FieldLabel>
                <Input
                  id={`${id}-path`}
                  name="repo_path"
                  defaultValue={project.repo_path}
                  autoComplete="off"
                  spellCheck={false}
                  placeholder="/Users/me/project"
                  className="font-mono text-[0.8125rem]"
                />
              </Field>
            )}
            {rename ? null : (
              <Field>
                <FieldLabel htmlFor={`${id}-description`}>{t('project.description')}</FieldLabel>
                <Textarea
                  id={`${id}-description`}
                  name="description"
                  defaultValue={project.description ?? ''}
                  rows={4}
                  maxLength={4000}
                  placeholder={t('project.descriptionPlaceholder')}
                  className="max-h-48 text-[0.8125rem]"
                />
                <FieldDescription>{t('project.descriptionHint')}</FieldDescription>
              </Field>
            )}
            {rename ? null : (
              <MaintainerFields project={project} id={id} member={maintainer} trigger={trigger} onMember={setMaintainer} onTrigger={setTrigger} />
            )}
            {rename ? null : (
              <Field>
                <FieldLabel htmlFor={`${id}-mounts`}>{t('project.mounts')}</FieldLabel>
                <Textarea
                  id={`${id}-mounts`}
                  name="external_bundles"
                  defaultValue={(project.wiki_external_bundles ?? []).join('\n')}
                  rows={2}
                  spellCheck={false}
                  placeholder="/data/catalog"
                  className="max-h-40 font-mono text-[0.8125rem]"
                />
                <FieldDescription>{t('project.mountsHint')}</FieldDescription>
              </Field>
            )}
            {error ? <FieldError className="wrap-anywhere">{error}</FieldError> : null}
          </FieldGroup>
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

// maintainerChange is what the request says of the wiki maintainer: only
// what changed.
function maintainerChange(project: Project, member: string, trigger: UpkeepTrigger) {
  const change: { wiki_maintainer_member_id?: string; wiki_maintainer_trigger?: UpkeepTrigger } = {}
  if (member !== (project.wiki_maintainer_member_id ?? '')) change.wiki_maintainer_member_id = member
  if (member !== '' && trigger !== (project.wiki_maintainer_trigger ?? 'daily')) change.wiki_maintainer_trigger = trigger
  return change
}

// mountsChange is what the request says of the bundles the wiki mounts:
// the folders, one a line, when they are not the ones it mounts already.
function mountsChange(project: Project, text: string) {
  const folders = text
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line !== '')
  const now = project.wiki_external_bundles ?? []
  const same = folders.length === now.length && folders.every((folder, i) => folder === now[i])
  return same ? {} : { wiki_external_bundles: folders }
}
