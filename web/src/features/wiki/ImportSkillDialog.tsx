import { useId, useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router'
import { toast } from 'sonner'
import { errorText } from '@/api/errorText'
import { useProjects } from '@/api/projects'
import { librarySpace, skillName, useImportSkill } from '@/api/wiki'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useT } from '@/lib/i18n'
import { pageHref } from './links'

// A select item cannot have the empty value; this stands for no team.
const noTeam = 'none'

// Adds a skill to the library from a folder on the machine Veyloom runs on
// (docs/design.md 5.15), looked after by a project's team or by none, then
// opens its page, where it is installed for agents.
export function ImportSkillDialog({ onClose }: { onClose: () => void }) {
  const t = useT()
  const id = useId()
  const navigate = useNavigate()
  const projects = useProjects()
  const importSkill = useImportSkill()
  const [folder, setFolder] = useState('')
  const [team, setTeam] = useState(noTeam)
  const [error, setError] = useState<string>()

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const path = folder.trim()
    if (path === '') {
      setError(t('library.import.folderRequired'))
      return
    }
    setError(undefined)
    importSkill.mutate(
      { folder: path, projectId: team === noTeam ? '' : team },
      {
        onSuccess: (page) => {
          toast.success(t('library.import.done', { name: skillName(page.path) }))
          onClose()
          void navigate(pageHref(librarySpace, page.path))
        },
        onError: (err) => setError(errorText(err)),
      },
    )
  }

  return (
    <Dialog open onOpenChange={(next) => (next ? undefined : onClose())}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={onSubmit}>
          <DialogHeader>
            <DialogTitle>{t('library.import.title')}</DialogTitle>
            <DialogDescription>{t('library.import.hint')}</DialogDescription>
          </DialogHeader>
          <FieldGroup className="my-4 gap-4">
            <Field data-invalid={error ? true : undefined}>
              <FieldLabel htmlFor={`${id}-folder`}>{t('library.import.folder')}</FieldLabel>
              <Input
                id={`${id}-folder`}
                value={folder}
                onChange={(event) => {
                  setFolder(event.target.value)
                  setError(undefined)
                }}
                placeholder="/Users/me/.claude/skills/release-notes"
                autoComplete="off"
                spellCheck={false}
                aria-invalid={error ? true : undefined}
                className="font-mono text-[0.8125rem]"
              />
              <FieldDescription>{t('library.import.folderHint')}</FieldDescription>
              {error ? <FieldError>{error}</FieldError> : null}
            </Field>
            <Field>
              <FieldLabel htmlFor={`${id}-team`}>{t('library.import.team')}</FieldLabel>
              <Select value={team} onValueChange={setTeam}>
                <SelectTrigger id={`${id}-team`} className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={noTeam}>{t('library.import.noTeam')}</SelectItem>
                  {(projects.data ?? []).map((project) => (
                    <SelectItem key={project.id} value={project.id}>
                      {project.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FieldDescription>{t('library.import.teamHint')}</FieldDescription>
            </Field>
          </FieldGroup>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={importSkill.isPending} aria-busy={importSkill.isPending}>
              {importSkill.isPending ? t('library.import.importing') : t('library.import.submit')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
