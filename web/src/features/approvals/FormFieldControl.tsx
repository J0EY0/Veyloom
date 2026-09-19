import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldContent, FieldDescription, FieldError, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { useT } from '@/lib/i18n'
import type { FormField, FormValue, Problem } from './forms'

export interface FormFieldControlProps {
  field: FormField
  id: string
  value: FormValue
  onChange: (value: FormValue) => void
  // Shown once the person has touched the field.
  problem?: Problem
  disabled?: boolean
}

// How many choices a field lists before it folds them into a dropdown.
const manyChoices = 5

// inputTypes maps an MCP string format onto the input that edits it.
const inputTypes: Record<string, string> = { email: 'email', uri: 'url', date: 'date', 'date-time': 'datetime-local' }

// One field of an MCP server's form, drawn by its type: text or a number
// in a line, yes or no as a switch, one choice as radios (a dropdown when
// there are many), several as checkboxes.
export function FormFieldControl({ field, id, value, onChange, problem, disabled }: FormFieldControlProps) {
  const t = useT()
  // One piece, so a label that lays its children out apart and a legend
  // that does not both put the mark right after the name.
  const title = (
    <span>
      {field.label}
      {field.required ? (
        <span aria-hidden="true" className="ms-0.5 text-status-fail">
          *
        </span>
      ) : null}
    </span>
  )
  const help = field.description ? <FieldDescription className="text-[0.75rem] leading-snug">{field.description}</FieldDescription> : null
  const error = problem ? <FieldError className="text-[0.75rem]">{t(problem.key, { n: problem.n ?? 0 })}</FieldError> : null

  if (field.type === 'boolean') {
    return (
      <Field orientation="horizontal" className="gap-2.5">
        <Switch id={id} size="sm" checked={value === true} onCheckedChange={(checked) => onChange(checked)} disabled={disabled} />
        <FieldContent className="gap-0.5">
          <FieldLabel htmlFor={id} className="text-[0.8125rem] font-normal">
            {title}
          </FieldLabel>
          {help}
        </FieldContent>
      </Field>
    )
  }

  if (field.type === 'choice' || field.type === 'choices') {
    const picked = field.type === 'choices' ? (value as string[]) : []
    return (
      <FieldSet className="gap-1.5" data-invalid={problem ? true : undefined}>
        <FieldLegend variant="label" className="mb-0 text-[0.8125rem]">
          {title}
        </FieldLegend>
        {help}
        {field.type === 'choices' ? (
          <div className="flex flex-col gap-1.5">
            {field.choices.map((choice, index) => (
              <Field key={choice.value} orientation="horizontal" className="gap-2">
                <Checkbox
                  id={`${id}-${index}`}
                  checked={picked.includes(choice.value)}
                  disabled={disabled}
                  onCheckedChange={(checked) => onChange(checked === true ? [...picked, choice.value] : picked.filter((p) => p !== choice.value))}
                />
                <FieldLabel htmlFor={`${id}-${index}`} className="text-[0.8125rem] font-normal">
                  {choice.label}
                </FieldLabel>
              </Field>
            ))}
          </div>
        ) : field.choices.length > manyChoices ? (
          <Select value={value as string} onValueChange={onChange} disabled={disabled} required={field.required}>
            <SelectTrigger id={id} size="sm" aria-label={field.label} className="w-full bg-background text-[0.8125rem]">
              <SelectValue placeholder={t('form.pick')} />
            </SelectTrigger>
            <SelectContent>
              {field.choices.map((choice) => (
                <SelectItem key={choice.value} value={choice.value}>
                  {choice.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : (
          <RadioGroup
            aria-label={field.label}
            value={value as string}
            onValueChange={onChange}
            disabled={disabled}
            required={field.required}
            className="gap-1.5"
          >
            {field.choices.map((choice, index) => (
              <Field key={choice.value} orientation="horizontal" className="gap-2">
                <RadioGroupItem id={`${id}-${index}`} value={choice.value} />
                <FieldLabel htmlFor={`${id}-${index}`} className="text-[0.8125rem] font-normal">
                  {choice.label}
                </FieldLabel>
              </Field>
            ))}
          </RadioGroup>
        )}
        {error}
      </FieldSet>
    )
  }

  return (
    <Field className="gap-1.5" data-invalid={problem ? true : undefined}>
      <FieldLabel htmlFor={id} className="text-[0.8125rem]">
        {title}
      </FieldLabel>
      {help}
      <Input
        id={id}
        type={field.type === 'number' ? 'number' : (inputTypes[field.format ?? ''] ?? 'text')}
        inputMode={field.type === 'number' ? (field.integer ? 'numeric' : 'decimal') : undefined}
        min={field.type === 'number' ? field.minimum : undefined}
        max={field.type === 'number' ? field.maximum : undefined}
        step={field.type === 'number' ? (field.integer ? 1 : 'any') : undefined}
        value={value as string}
        onChange={(event) => onChange(event.target.value)}
        disabled={disabled}
        aria-invalid={problem ? true : undefined}
        aria-required={field.required || undefined}
        autoComplete="off"
        spellCheck={false}
        className="h-7 bg-background text-[0.78125rem]"
      />
      {error}
    </Field>
  )
}
