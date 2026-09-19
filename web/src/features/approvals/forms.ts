import type { Approval } from '@/api/types'
import type { MessageKey } from '@/i18n/zh-CN'

// What an MCP server asks a person to fill in or open (MCP elicitation),
// as the runtime passed it on: the server's schema as it came
// (internal/runtime/elicitation.go).
export interface FormRequest {
  server: string
  message: string
  fields: FormField[]
}

export interface LinkRequest {
  server: string
  message: string
  url: string
}

export interface Choice {
  value: string
  label: string
}

interface FieldBase {
  name: string
  label: string
  description?: string
  required: boolean
}

// FormField is one field of an MCP elicitation form: the spec allows
// text, numbers, yes or no, and one or several of a set of choices.
export type FormField =
  | (FieldBase & { type: 'text'; format?: string; minLength?: number; maxLength?: number; initial: string })
  | (FieldBase & { type: 'number'; integer: boolean; minimum?: number; maximum?: number; initial: string })
  | (FieldBase & { type: 'boolean'; initial: boolean })
  | (FieldBase & { type: 'choice'; choices: Choice[]; initial: string })
  | (FieldBase & { type: 'choices'; choices: Choice[]; minItems?: number; maxItems?: number; initial: string[] })

export type FormValue = string | boolean | string[]

export interface Problem {
  key: MessageKey
  n?: number
}

type Schema = Record<string, unknown>

export function formOf(approval: Pick<Approval, 'kind' | 'input'>): FormRequest | undefined {
  if (approval.kind !== 'form') return undefined
  const input = parsed(approval.input)
  return { server: text(input.server), message: text(input.message), fields: fieldsOf(input.schema) }
}

export function linkOf(approval: Pick<Approval, 'kind' | 'input'>): LinkRequest | undefined {
  if (approval.kind !== 'link') return undefined
  const input = parsed(approval.input)
  return { server: text(input.server), message: text(input.message), url: text(input.url) }
}

// fieldsOf reads an elicitation schema's fields in the order it lists them.
export function fieldsOf(schema: unknown): FormField[] {
  const object = asObject(schema)
  const required = new Set(Array.isArray(object.required) ? object.required.map(String) : [])
  return Object.entries(asObject(object.properties)).map(([name, raw]) => fieldOf(name, asObject(raw), required.has(name)))
}

function fieldOf(name: string, s: Schema, required: boolean): FormField {
  const base = { name, label: text(s.title) || name, description: text(s.description) || undefined, required }
  const single = choicesOf(s)
  if (single) return { ...base, type: 'choice', choices: single, initial: text(s.default) }
  if (s.type === 'array') {
    return {
      ...base,
      type: 'choices',
      choices: choicesOf(asObject(s.items)) ?? [],
      minItems: number(s.minItems),
      maxItems: number(s.maxItems),
      initial: Array.isArray(s.default) ? s.default.map(String) : [],
    }
  }
  if (s.type === 'boolean') return { ...base, type: 'boolean', initial: s.default === true }
  if (s.type === 'number' || s.type === 'integer') {
    const initial = number(s.default)
    return {
      ...base,
      type: 'number',
      integer: s.type === 'integer',
      minimum: number(s.minimum),
      maximum: number(s.maximum),
      initial: initial === undefined ? '' : String(initial),
    }
  }
  return {
    ...base,
    type: 'text',
    format: text(s.format) || undefined,
    minLength: number(s.minLength),
    maxLength: number(s.maxLength),
    initial: text(s.default),
  }
}

// choicesOf reads the choices of a string field, however the spec lets a
// server write them; undefined when the field is free text.
function choicesOf(s: Schema): Choice[] | undefined {
  if (Array.isArray(s.oneOf)) return s.oneOf.map((o) => ({ value: text(asObject(o).const), label: text(asObject(o).title) || text(asObject(o).const) }))
  if (Array.isArray(s.anyOf)) return s.anyOf.map((o) => ({ value: text(asObject(o).const), label: text(asObject(o).title) || text(asObject(o).const) }))
  if (Array.isArray(s.enum)) {
    const names = Array.isArray(s.enumNames) ? s.enumNames : []
    return s.enum.map((value, i) => ({ value: String(value), label: text(names[i]) || String(value) }))
  }
  return undefined
}

// problemOf says what is wrong with a field's value, if anything.
export function problemOf(field: FormField, value: FormValue): Problem | undefined {
  switch (field.type) {
    case 'boolean':
      return undefined
    case 'choices': {
      const picked = value as string[]
      if (field.required && picked.length === 0) return { key: 'form.required' }
      if (picked.length > 0 && field.minItems !== undefined && picked.length < field.minItems) return { key: 'form.tooFew', n: field.minItems }
      if (field.maxItems !== undefined && picked.length > field.maxItems) return { key: 'form.tooMany', n: field.maxItems }
      return undefined
    }
    default: {
      const entered = (value as string).trim()
      if (entered === '') return field.required ? { key: 'form.required' } : undefined
      if (field.type === 'number') {
        const n = Number(entered)
        if (!Number.isFinite(n)) return { key: 'form.notNumber' }
        if (field.integer && !Number.isInteger(n)) return { key: 'form.notInteger' }
        if (field.minimum !== undefined && n < field.minimum) return { key: 'form.tooSmall', n: field.minimum }
        if (field.maximum !== undefined && n > field.maximum) return { key: 'form.tooLarge', n: field.maximum }
        return undefined
      }
      if (field.type === 'text') {
        if (field.minLength !== undefined && entered.length < field.minLength) return { key: 'form.tooShort', n: field.minLength }
        if (field.maxLength !== undefined && entered.length > field.maxLength) return { key: 'form.tooLong', n: field.maxLength }
        if (field.format === 'email' && !/^[^\s@]+@[^\s@]+$/.test(entered)) return { key: 'form.badEmail' }
        if (field.format === 'uri' && !isURL(entered)) return { key: 'form.badUrl' }
      }
      return undefined
    }
  }
}

// contentOf is what goes back to the server: each field as its type, the
// optional ones left empty left out.
export function contentOf(fields: FormField[], values: Record<string, FormValue>): Record<string, unknown> {
  const content: Record<string, unknown> = {}
  for (const field of fields) {
    const value = values[field.name] ?? field.initial
    if (field.type === 'boolean') {
      content[field.name] = value === true
    } else if (field.type === 'choices') {
      const picked = value as string[]
      if (picked.length > 0 || field.required) content[field.name] = picked
    } else {
      const entered = (value as string).trim()
      if (entered === '') continue
      if (field.type === 'number') content[field.name] = Number(entered)
      else if (field.type === 'text' && field.format === 'date-time') content[field.name] = new Date(entered).toISOString()
      else content[field.name] = entered
    }
  }
  return content
}

// isWebLink says whether a link may be opened from a card: only http and
// https, never a script or a local file.
export function isWebLink(url: string): boolean {
  try {
    const { protocol } = new URL(url)
    return protocol === 'http:' || protocol === 'https:'
  } catch {
    return false
  }
}

function isURL(value: string): boolean {
  try {
    new URL(value)
    return true
  } catch {
    return false
  }
}

function parsed(input: unknown): Schema {
  if (typeof input !== 'string') return asObject(input)
  try {
    return asObject(JSON.parse(input))
  } catch {
    return {}
  }
}

function asObject(value: unknown): Schema {
  return value !== null && typeof value === 'object' && !Array.isArray(value) ? (value as Schema) : {}
}

function text(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function number(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
}
