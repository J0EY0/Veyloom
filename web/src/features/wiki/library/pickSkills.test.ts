import { describe, expect, it } from 'vitest'
import { leftOut, pickedFiles } from './pickSkills'

// A folder picked to upload keeps a skill's hidden files, its settings and
// templates, and leaves out what the import would: version control's own
// files, what an operating system leaves behind, and environment files but
// for an example one.
describe('pickedFiles', () => {
  it('keeps hidden files the import keeps', () => {
    const named = (path: string) => {
      const file = new File(['x'], path.split('/').pop() ?? path)
      Object.defineProperty(file, 'webkitRelativePath', { value: path })
      return file
    }
    const picked = pickedFiles(
      [
        'tools/SKILL.md',
        'tools/.eslintrc.json',
        'tools/.github/workflow.yml',
        'tools/.env.example',
        'tools/.env',
        'tools/.env.local',
        'tools/.gitignore',
        'tools/.git/config',
        'tools/.DS_Store',
      ].map(named),
    )
    const body = picked?.body
    expect(body && 'files' in body ? body.files.map(({ path }) => path) : []).toEqual([
      'tools/SKILL.md',
      'tools/.eslintrc.json',
      'tools/.github/workflow.yml',
      'tools/.env.example',
    ])
    expect(picked?.count).toBe(4)
  })

  it('names what is left out', () => {
    expect(['.git', '.gitignore', '.gitattributes', '.DS_Store', '.env', '.env.production'].every(leftOut)).toBe(true)
    expect(['.venv', '.tox', '.cache', '.pytest_cache', '.ruff_cache', '.parcel-cache'].every(leftOut)).toBe(true)
    expect(['.env.example', '.env.sample', '.eslintrc', '.github', 'SKILL.md', 'venv', '.vscode'].some(leftOut)).toBe(false)
  })
})
