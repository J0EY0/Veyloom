import { useId } from 'react'
import { errorText } from '@/api/errorText'
import { librarySpace, useWikiCatalog } from '@/api/wiki'
import { Badge } from '@/components/ui/badge'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldContent, FieldDescription, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Spinner } from '@/components/ui/spinner'
import { skillChoices, type SkillChoice } from '@/features/wiki/skills'
import { useT } from '@/lib/i18n'
import { runtimeName } from '@/lib/runtimes'

export interface SkillsFieldProps {
  // The agent's runtime, which decides what it may have.
  runtime: string
  // The skills installed, by name.
  value: string[]
  onChange: (skills: string[]) => void
}

// The skills of the library installed for an agent (docs/design.md 5.15):
// the ones for its runtime to pick from, and any it has that it is given
// no longer, to take off.
export function SkillsField({ runtime, value, onChange }: SkillsFieldProps) {
  const t = useT()
  const id = useId()
  const catalog = useWikiCatalog(librarySpace)
  const choices = skillChoices(catalog.data, runtime, value)

  function problem(skill: SkillChoice): string | undefined {
    switch (skill.problem) {
      case 'gone':
        return t('agent.skillGone')
      case 'retired':
        return t('agent.skillRetired')
      case 'otherRuntime':
        return t('agent.skillOtherRuntime', { runtimes: skill.runtimes.map(runtimeName).join('、') })
    }
    return undefined
  }

  return (
    <FieldSet className="gap-2">
      <FieldLegend variant="label" className="mb-0">
        {t('agent.skills')}
      </FieldLegend>
      <FieldDescription>{t('agent.skillsHint')}</FieldDescription>
      {catalog.isPending ? (
        <p role="status" className="flex items-center gap-2 py-1 text-xs text-subtle">
          <Spinner className="size-3" />
          {t('common.loading')}
        </p>
      ) : catalog.isError ? (
        <p className="text-[0.8125rem] text-destructive">{t('agent.skillsFailed', { error: errorText(catalog.error) })}</p>
      ) : choices.length === 0 ? (
        <p className="text-[0.8125rem] text-subtle">{t('agent.noSkills')}</p>
      ) : (
        <ul className="max-h-56 divide-y overflow-y-auto rounded-md border">
          {choices.map((skill) => {
            const box = `${id}-${skill.name}`
            const why = problem(skill)
            return (
              <li key={skill.name}>
                <Field orientation="horizontal" className="px-3 py-2">
                  <Checkbox
                    id={box}
                    checked={value.includes(skill.name)}
                    onCheckedChange={(on) => onChange(on === true ? [...value, skill.name] : value.filter((name) => name !== skill.name))}
                  />
                  <FieldContent className="min-w-0 gap-0.5">
                    <FieldLabel htmlFor={box} className="flex-wrap font-normal">
                      {skill.title}
                      <span className="font-mono text-xs text-subtle" translate="no">
                        {skill.name}
                      </span>
                      {skill.draft ? <Badge variant="outline">{t('agent.skillDraft')}</Badge> : null}
                    </FieldLabel>
                    {why ? (
                      <FieldDescription className="text-xs text-status-wait">{why}</FieldDescription>
                    ) : skill.description ? (
                      <FieldDescription className="line-clamp-2 text-xs">{skill.description}</FieldDescription>
                    ) : null}
                  </FieldContent>
                </Field>
              </li>
            )
          })}
        </ul>
      )}
    </FieldSet>
  )
}
