import type { Question } from '@/api/types'
import { Badge } from '@/components/ui/badge'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldContent, FieldDescription, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Textarea } from '@/components/ui/textarea'
import { useT } from '@/lib/i18n'
import { OTHER } from './questions'

export interface QuestionFieldProps {
  question: Question
  // Prefixes the ids that tie each choice to its label.
  idPrefix: string
  picked: string[]
  written: string
  onPick: (picked: string[]) => void
  onWrite: (written: string) => void
  disabled?: boolean
}

// One question on a question card: its header and text, the options to
// pick from (one, or several when it says so), and a line to write an
// answer of one's own, masked when the answer is a secret, or a box for
// text of several lines.
export function QuestionField({ question, idPrefix, picked, written, onPick, onWrite, disabled }: QuestionFieldProps) {
  const t = useT()
  const options = question.options ?? []
  const choices = [
    ...options.map((option) => ({ value: option.label, label: option.label, description: option.description })),
    ...(options.length > 0 && question.other ? [{ value: OTHER, label: t('question.other'), description: undefined }] : []),
  ]
  const writing = options.length === 0 || picked.includes(OTHER)
  const rows = choices.map((choice, index) => {
    const id = `${idPrefix}-${index}`
    return (
      <Field key={choice.value} orientation="horizontal" className="gap-2">
        {question.multiSelect ? (
          <Checkbox
            id={id}
            checked={picked.includes(choice.value)}
            disabled={disabled}
            onCheckedChange={(checked) => onPick(checked === true ? [...picked, choice.value] : picked.filter((p) => p !== choice.value))}
          />
        ) : (
          <RadioGroupItem id={id} value={choice.value} disabled={disabled} />
        )}
        <FieldContent className="gap-0.5">
          <FieldLabel htmlFor={id} className="text-[0.8125rem] font-normal">
            {choice.label}
          </FieldLabel>
          {choice.description ? <FieldDescription className="text-[0.75rem] leading-snug">{choice.description}</FieldDescription> : null}
        </FieldContent>
      </Field>
    )
  })

  return (
    <FieldSet className="gap-2">
      <FieldLegend variant="label" className="mb-0.5 flex flex-wrap items-center gap-1.5 text-[0.8125rem] leading-snug font-medium">
        {question.header ? (
          <Badge variant="secondary" className="h-5 rounded-sm px-1.5 text-[0.6875rem] font-normal">
            {question.header}
          </Badge>
        ) : null}
        <span>{question.question}</span>
      </FieldLegend>
      {choices.length > 0 && question.multiSelect ? <div className="flex flex-col gap-2">{rows}</div> : null}
      {choices.length > 0 && !question.multiSelect ? (
        <RadioGroup aria-label={question.question} value={picked[0] ?? ''} onValueChange={(value) => onPick([value])} className="gap-2">
          {rows}
        </RadioGroup>
      ) : null}
      {writing && question.multiline ? (
        <Textarea
          value={written}
          onChange={(event) => onWrite(event.target.value)}
          disabled={disabled}
          aria-label={t('question.write', { question: question.question })}
          placeholder={question.placeholder || t('question.writePlaceholder')}
          spellCheck={false}
          rows={5}
          className="min-h-24 bg-background font-mono text-[0.78125rem]"
        />
      ) : writing ? (
        <Input
          type={question.secret ? 'password' : 'text'}
          value={written}
          onChange={(event) => onWrite(event.target.value)}
          disabled={disabled}
          aria-label={t('question.write', { question: question.question })}
          placeholder={question.secret ? t('question.secretPlaceholder') : question.placeholder || t('question.writePlaceholder')}
          autoComplete={question.secret ? 'new-password' : 'off'}
          spellCheck={false}
          className="h-7 bg-background text-[0.78125rem]"
        />
      ) : null}
    </FieldSet>
  )
}
