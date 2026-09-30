import { useId, useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router'
import { toast } from 'sonner'
import { errorText, problemText } from '@/api/errorText'
import { useProjects } from '@/api/projects'
import { librarySpace, skillName, useImportSkill, useInstallSkill, useLocalSkills, useUploadSkills } from '@/api/wiki'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useT } from '@/lib/i18n'
import { AgentPicker, LocalList, UploadPicker } from './library/ImportParts'
import type { Picked } from './library/pickSkills'
import { pageHref } from './links'

// A select item cannot have the empty value; this stands for no team.
const noTeam = 'none'

type Way = 'local' | 'upload' | 'folder'

// Adds skills to the library (docs/design.md 5.15), three ways: ticked
// from the ones people keep for their runtimes on this machine, uploaded
// as a zip or a folder from the browser, or read from a folder typed in.
// Whichever way, they are looked after by a project's team or by none, and
// installed at once for the agents picked. One skill imported opens its
// page; several stay in the list.
export function ImportSkillDialog({ onClose }: { onClose: () => void }) {
  const t = useT()
  const id = useId()
  const navigate = useNavigate()
  const projects = useProjects()
  const importSkill = useImportSkill()
  const uploadSkills = useUploadSkills()
  const install = useInstallSkill()
  const local = useLocalSkills()
  const [way, setWay] = useState<Way>('local')
  const [ticked, setTicked] = useState<string[]>([])
  const [picked, setPicked] = useState<Picked>()
  const [folder, setFolder] = useState('')
  const [team, setTeam] = useState(noTeam)
  const [agents, setAgents] = useState<string[]>([])
  const [errors, setErrors] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const projectId = team === noTeam ? '' : team

  const ready = way === 'local' ? ticked.length > 0 : way === 'upload' ? picked !== undefined : true

  // imports brings the skills in the way chosen, and says which came in
  // (their pages) and why the others did not.
  async function imports(): Promise<{ pages: string[]; failed: string[] }> {
    const pages: string[] = []
    const failed: string[] = []
    if (way === 'upload' && picked) {
      const outcomes = await uploadSkills.mutateAsync({ upload: picked.body, projectId })
      for (const skill of outcomes) {
        if (skill.path) pages.push(skill.path)
        else failed.push(t('library.import.failed', { name: skill.name, error: problemText(skill.code, skill.params, skill.message ?? '') }))
      }
      return { pages, failed }
    }
    const folders = way === 'local' ? ticked : [folder.trim()]
    for (const one of folders) {
      try {
        pages.push((await importSkill.mutateAsync({ folder: one, projectId })).path)
      } catch (err) {
        failed.push(way === 'local' ? t('library.import.failed', { name: nameOf(one), error: errorText(err) }) : errorText(err))
      }
    }
    return { pages, failed }
  }

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (way === 'folder' && folder.trim() === '') {
      setErrors([t('library.import.folderRequired')])
      return
    }
    setErrors([])
    setBusy(true)
    try {
      const { pages, failed } = await imports()
      // Installed at once for the agents picked.
      for (const page of pages) {
        for (const agentId of agents) {
          try {
            await install.mutateAsync({ name: skillName(page), agentId, installed: true })
          } catch (err) {
            failed.push(t('skill.installFailed', { error: errorText(err) }))
          }
        }
      }
      if (pages.length === 1) toast.success(t('library.import.done', { name: skillName(pages[0]) }))
      if (pages.length > 1) toast.success(t('library.import.doneMany', { n: pages.length }))
      if (failed.length > 0) {
        setErrors(failed)
        return
      }
      onClose()
      if (pages.length === 1) void navigate(pageHref(librarySpace, pages[0]))
    } catch (err) {
      setErrors([errorText(err)])
    } finally {
      setBusy(false)
    }
  }

  function nameOf(path: string): string {
    return local.data?.find((skill) => skill.folder === path)?.name ?? path
  }

  return (
    <Dialog open onOpenChange={(next) => (next ? undefined : onClose())}>
      <DialogContent className="sm:max-w-xl">
        <form onSubmit={(event) => void onSubmit(event)}>
          <DialogHeader>
            <DialogTitle>{t('library.import.title')}</DialogTitle>
          </DialogHeader>
          <Tabs
            value={way}
            onValueChange={(next) => {
              setWay(next as Way)
              setErrors([])
            }}
            className="mt-4 gap-3"
          >
            <TabsList>
              <TabsTrigger value="local">{t('library.import.tab.local')}</TabsTrigger>
              <TabsTrigger value="upload">{t('library.import.tab.upload')}</TabsTrigger>
              <TabsTrigger value="folder">{t('library.import.tab.folder')}</TabsTrigger>
            </TabsList>
            <TabsContent value="local" className="min-h-48">
              <LocalList
                skills={local.data}
                loading={local.isPending}
                failed={local.isError ? errorText(local.error) : undefined}
                ticked={ticked}
                onTick={setTicked}
              />
            </TabsContent>
            <TabsContent value="upload" className="min-h-48">
              <UploadPicker picked={picked} onPick={setPicked} />
            </TabsContent>
            <TabsContent value="folder" className="min-h-48">
              <Field>
                <FieldLabel htmlFor={`${id}-folder`}>{t('library.import.folder')}</FieldLabel>
                <Input
                  id={`${id}-folder`}
                  value={folder}
                  onChange={(event) => {
                    setFolder(event.target.value)
                    setErrors([])
                  }}
                  placeholder="/Users/me/.claude/skills/release-notes"
                  autoComplete="off"
                  spellCheck={false}
                  className="font-mono text-[0.8125rem]"
                />
                <FieldDescription>{t('library.import.folderHint')}</FieldDescription>
              </Field>
            </TabsContent>
          </Tabs>
          <FieldGroup className="mt-4 mb-4 gap-4 sm:flex-row">
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
            </Field>
            <Field>
              <FieldLabel htmlFor={`${id}-agents`}>{t('library.import.agents')}</FieldLabel>
              <AgentPicker id={`${id}-agents`} value={agents} onChange={setAgents} />
            </Field>
          </FieldGroup>
          {errors.length > 0 ? <FieldError className="mb-4 text-[0.8125rem] whitespace-pre-line">{errors.join('\n')}</FieldError> : null}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={busy || !ready} aria-busy={busy}>
              {busy
                ? t('library.import.importing')
                : way === 'local' && ticked.length > 1
                  ? t('library.import.submitMany', { n: ticked.length })
                  : t('library.import.submit')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
