import type { BundledLanguage } from 'shiki'
import type { AttachmentKind } from '@/api/types'

// The attachments tab's kinds of file, each a few kinds (docs/webui.md
// 4.21): pictures and video, documents, the rest.
export type KindGroup = 'all' | 'media' | 'docs' | 'other'

export const kindGroups: Record<KindGroup, AttachmentKind[]> = {
  all: [],
  media: ['image', 'video'],
  docs: ['pdf', 'text', 'office'],
  other: ['audio', 'archive', 'other'],
}

const fallbackMarks: Record<AttachmentKind, string> = {
  image: 'IMG',
  video: 'VIDEO',
  audio: 'AUDIO',
  pdf: 'PDF',
  text: 'TXT',
  office: 'DOC',
  archive: 'ZIP',
  other: 'FILE',
}

// extensionOf is a file's mark: its extension in capitals, or its kind
// when the name has none short enough.
export function extensionOf(filename: string, kind: AttachmentKind): string {
  const dot = filename.lastIndexOf('.')
  const ext = dot > 0 ? filename.slice(dot + 1) : ''
  return ext !== '' && ext.length <= 5 ? ext.toUpperCase() : fallbackMarks[kind]
}

// languages are the highlighters shiki bundles, by extension; go.mod and
// the like, which it has none for, are read as plain text.
export const languages: Readonly<Record<string, BundledLanguage>> = {
  go: 'go',
  ts: 'typescript',
  tsx: 'tsx',
  js: 'javascript',
  mjs: 'javascript',
  cjs: 'javascript',
  jsx: 'jsx',
  py: 'python',
  rb: 'ruby',
  rs: 'rust',
  java: 'java',
  kt: 'kotlin',
  swift: 'swift',
  c: 'c',
  h: 'c',
  cc: 'cpp',
  cpp: 'cpp',
  hpp: 'cpp',
  cs: 'csharp',
  php: 'php',
  sh: 'bash',
  bash: 'bash',
  zsh: 'bash',
  fish: 'fish',
  sql: 'sql',
  json: 'json',
  jsonl: 'json',
  yaml: 'yaml',
  yml: 'yaml',
  toml: 'toml',
  ini: 'ini',
  xml: 'xml',
  html: 'html',
  htm: 'html',
  css: 'css',
  scss: 'scss',
  md: 'markdown',
  markdown: 'markdown',
  diff: 'diff',
  patch: 'diff',
  proto: 'proto',
  graphql: 'graphql',
  vue: 'vue',
  svelte: 'svelte',
  lua: 'lua',
  r: 'r',
  scala: 'scala',
  dart: 'dart',
  csv: 'csv',
  log: 'log',
}

// languageOf is how a text file is highlighted, by its name; plain text
// when the name says nothing.
export function languageOf(filename: string): BundledLanguage {
  const dot = filename.lastIndexOf('.')
  const ext = dot > 0 ? filename.slice(dot + 1).toLowerCase() : ''
  return languages[ext] ?? ('text' as BundledLanguage)
}

// isMarkdown says a text file is drawn as a document, not as code.
export function isMarkdown(filename: string): boolean {
  return /\.(md|markdown)$/i.test(filename)
}

// lineCount counts a text's lines; a last line without its break counts.
export function lineCount(text: string): number {
  if (text === '') return 0
  const breaks = text.split('\n').length - 1
  return text.endsWith('\n') ? breaks : breaks + 1
}

// firstLines is a text's first n lines.
export function firstLines(text: string, n: number): string {
  return text.split('\n').slice(0, n).join('\n')
}

// withoutLastBreak is a text as its lines are numbered: the break that
// ends the last line starts no line of its own.
export function withoutLastBreak(text: string): string {
  if (text.endsWith('\r\n')) return text.slice(0, -2)
  return text.endsWith('\n') ? text.slice(0, -1) : text
}
