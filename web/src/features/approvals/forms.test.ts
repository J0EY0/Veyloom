import { describe, expect, it } from 'vitest'
import { approval } from '@/test/fixtures'
import { approvalCommand } from './describe'
import { contentOf, fieldsOf, formOf, isWebLink, linkOf, problemOf } from './forms'

// One field of every kind MCP elicitation allows (internal/runtime/fake.go).
const schema = {
  type: 'object',
  properties: {
    name: { type: 'string', title: 'Name', description: 'Who is deploying', minLength: 2 },
    size: {
      type: 'string',
      title: 'Size',
      oneOf: [
        { const: 's', title: 'Small' },
        { const: 'l', title: 'Large' },
      ],
    },
    tier: { type: 'string', enum: ['free', 'pro'], enumNames: ['Free', 'Pro'] },
    regions: { type: 'array', title: 'Regions', items: { type: 'string', enum: ['eu', 'us'] }, minItems: 1 },
    count: { type: 'integer', title: 'Count', minimum: 1, maximum: 5, default: 1 },
    notify: { type: 'boolean', title: 'Notify me', default: true },
    email: { type: 'string', format: 'email' },
  },
  required: ['name', 'size'],
}

describe('forms', () => {
  it('reads every kind of field an elicitation schema allows', () => {
    const fields = fieldsOf(schema)
    expect(fields.map((f) => [f.name, f.type, f.label, f.required])).toEqual([
      ['name', 'text', 'Name', true],
      ['size', 'choice', 'Size', true],
      ['tier', 'choice', 'tier', false],
      ['regions', 'choices', 'Regions', false],
      ['count', 'number', 'Count', false],
      ['notify', 'boolean', 'Notify me', false],
      ['email', 'text', 'email', false],
    ])
    expect(fields[1]).toMatchObject({
      choices: [
        { value: 's', label: 'Small' },
        { value: 'l', label: 'Large' },
      ],
    })
    expect(fields[2]).toMatchObject({
      choices: [
        { value: 'free', label: 'Free' },
        { value: 'pro', label: 'Pro' },
      ],
    })
    expect(fields[3]).toMatchObject({
      choices: [
        { value: 'eu', label: 'eu' },
        { value: 'us', label: 'us' },
      ],
      minItems: 1,
    })
    expect(fields[4]).toMatchObject({ integer: true, minimum: 1, maximum: 5, initial: '1' })
    expect(fields[5]).toMatchObject({ initial: true })
    expect(fieldsOf('nonsense')).toEqual([])
  })

  it('says what is wrong with a value', () => {
    const [name, size, , regions, count, notify, email] = fieldsOf(schema)
    expect(problemOf(name, '')).toEqual({ key: 'form.required' })
    expect(problemOf(name, 'a')).toEqual({ key: 'form.tooShort', n: 2 })
    expect(problemOf(name, 'alice')).toBeUndefined()
    expect(problemOf(size, '')).toEqual({ key: 'form.required' })
    expect(problemOf(regions, [])).toBeUndefined()
    expect(problemOf(count, 'x')).toEqual({ key: 'form.notNumber' })
    expect(problemOf(count, '1.5')).toEqual({ key: 'form.notInteger' })
    expect(problemOf(count, '9')).toEqual({ key: 'form.tooLarge', n: 5 })
    expect(problemOf(notify, false)).toBeUndefined()
    expect(problemOf(email, 'nope')).toEqual({ key: 'form.badEmail' })
    expect(problemOf(email, '')).toBeUndefined()
  })

  it('sends each field as its type, leaving out optional ones left empty', () => {
    const fields = fieldsOf(schema)
    expect(contentOf(fields, { name: ' alice ', size: 'l', regions: ['eu'], count: '3', notify: false })).toEqual({
      name: 'alice',
      size: 'l',
      regions: ['eu'],
      count: 3,
      notify: false,
    })
    // Untouched fields take their defaults.
    expect(contentOf(fields, { name: 'bob', size: 's' })).toEqual({ name: 'bob', size: 's', count: 1, notify: true })
  })

  it('reads forms and links, and only opens web links', () => {
    const form = approval('f1', { kind: 'form', tool: 'elicitation', input: { server: 'deploy', message: 'Deploy where?', schema } })
    expect(formOf(form)?.fields).toHaveLength(7)
    expect(approvalCommand(form)).toBe('Deploy where?')
    const link = approval('l1', {
      kind: 'link',
      tool: 'elicitation',
      input: JSON.stringify({ server: 'deploy', message: 'Sign in', url: 'https://login.example.com' }),
    })
    expect(linkOf(link)).toEqual({ server: 'deploy', message: 'Sign in', url: 'https://login.example.com' })
    expect(formOf(link)).toBeUndefined()
    expect(isWebLink('https://login.example.com/device')).toBe(true)
    expect(isWebLink('javascript:alert(1)')).toBe(false)
    expect(isWebLink('file:///etc/passwd')).toBe(false)
  })
})
