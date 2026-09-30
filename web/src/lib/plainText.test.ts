import { describe, expect, it } from 'vitest'
import { plainText } from './plainText'

describe('plainText', () => {
  it('takes code marks off, keeping the code', () => {
    expect(plainText('Please let `notes done` take **several** ids')).toBe('Please let notes done take several ids')
  })

  it('has no words for a field left out', () => {
    expect(plainText(undefined)).toBe('')
  })

  it('flattens fences, quotes, tables and rules', () => {
    expect(plainText('> 引用\n\n```go\nfmt.Println(1)\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n---\n1. 第一')).toBe('引用 fmt.Println(1) a b 1 2 第一')
  })
})
