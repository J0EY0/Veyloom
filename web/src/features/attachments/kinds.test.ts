import { describe, expect, it } from 'vitest'
import { bundledLanguages, bundledLanguagesAlias } from 'shiki'
import { extensionOf, firstLines, isMarkdown, languageOf, languages, lineCount, withoutLastBreak } from './kinds'

describe('what a file is called and read as', () => {
  it('marks a file by its extension, or by its kind when the name has none short enough', () => {
    expect(extensionOf('tags_test.go', 'text')).toBe('GO')
    expect(extensionOf('排期.xlsx', 'office')).toBe('XLSX')
    expect(extensionOf('README', 'text')).toBe('TXT')
    expect(extensionOf('.env', 'text')).toBe('TXT')
    expect(extensionOf('bundle.longextension', 'archive')).toBe('ZIP')
  })

  it('highlights code by its name, and anything else as plain text', () => {
    expect(languageOf('main.GO')).toBe('go')
    expect(languageOf('import-2026-09-25.log')).toBe('log')
    expect(languageOf('notes.txt')).toBe('text')
    expect(languageOf('Makefile')).toBe('text')
    expect(languageOf('go.mod')).toBe('text')
    expect(isMarkdown('NOTES.md')).toBe(true)
    expect(isMarkdown('guide.Markdown')).toBe(true)
    expect(isMarkdown('md.go')).toBe(false)
  })
})

describe('the lines of a text', () => {
  it('counts a last line with or without its break', () => {
    expect(lineCount('')).toBe(0)
    expect(lineCount('one')).toBe(1)
    expect(lineCount('one\ntwo\n')).toBe(2)
    expect(lineCount('one\ntwo')).toBe(2)
    expect(lineCount('\n')).toBe(1)
  })

  it('takes the first lines, and numbers no line after the last break', () => {
    expect(firstLines('a\nb\nc\nd', 2)).toBe('a\nb')
    expect(firstLines('a\nb', 9)).toBe('a\nb')
    expect(withoutLastBreak('a\nb\n')).toBe('a\nb')
    expect(withoutLastBreak('a\r\nb\r\n')).toBe('a\r\nb')
    expect(withoutLastBreak('a\nb')).toBe('a\nb')
    expect(withoutLastBreak('a\n\n')).toBe('a\n')
  })
})

// A language shiki lacks leaves the block empty: every one named is one it
// has.
it('highlights only in languages shiki has', () => {
  const known = new Set([...Object.keys(bundledLanguages), ...Object.keys(bundledLanguagesAlias)])
  expect(Object.values(languages).filter((l) => !known.has(l))).toEqual([])
})
