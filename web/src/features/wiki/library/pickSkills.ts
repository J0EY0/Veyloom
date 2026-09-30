import type { SkillUploadBody } from '@/api/wiki'

// What a person picked to upload to the library (docs/design.md 5.15): a
// zip file, or the files of a folder, each with its path from the folder's
// name on. name is the zip's or the folder's; count how many files; size
// their bytes.
export interface Picked {
  name: string
  count: number
  size: number
  body: SkillUploadBody
}

function isZip(file: File): boolean {
  return file.name.toLowerCase().endsWith('.zip') || file.type === 'application/zip' || file.type === 'application/x-zip-compressed'
}

// pickedFiles reads what a file input gave: one zip file, or a folder's
// files (an input with webkitdirectory), whose paths the browser keeps.
export function pickedFiles(files: FileList | File[]): Picked | undefined {
  const list = Array.from(files)
  if (list.length === 1 && !list[0].webkitRelativePath && isZip(list[0])) {
    return { name: list[0].name, count: 1, size: list[0].size, body: { zip: list[0] } }
  }
  return folderPicked(list.map((file) => ({ path: file.webkitRelativePath || file.name, file })))
}

// leftOut names what the import leaves out of a skill anyway (wiki.
// SkillLeavesOut): version control's own files, what an operating system
// leaves behind, the environments and caches tools leave where they ran,
// and environment files but for an .env.example. A skill's other hidden
// files come along.
const leftOutNames = new Set([
  ...['.git', '.gitignore', '.gitattributes', '.gitmodules', '.hg', '.svn'],
  ...['.DS_Store', 'Thumbs.db', '__MACOSX'],
  ...['.venv', '.tox', '.nox', '.cache', '.ipynb_checkpoints', '.eslintcache'],
  '.env',
])
const keptEnv = new Set(['.env.example', '.env.sample', '.env.template'])

export function leftOut(name: string): boolean {
  if (leftOutNames.has(name)) return true
  if (name.startsWith('.') && (name.endsWith('_cache') || name.endsWith('-cache'))) return true
  return name.startsWith('.env.') && !keptEnv.has(name)
}

// folderPicked leaves out what the import would leave behind anyway.
function folderPicked(files: { path: string; file: File }[]): Picked | undefined {
  const kept = files.filter(({ path }) => !path.split('/').some(leftOut))
  if (kept.length === 0) return undefined
  return {
    name: kept[0].path.split('/')[0],
    count: kept.length,
    size: kept.reduce((sum, { file }) => sum + file.size, 0),
    body: { files: kept },
  }
}

// droppedFiles reads what was dropped: a zip file, or folders, walked
// through with the entries browsers give for them.
export async function droppedFiles(transfer: DataTransfer): Promise<Picked | undefined> {
  const entries = Array.from(transfer.items ?? [])
    .map((item) => item.webkitGetAsEntry?.())
    .filter((entry): entry is FileSystemEntry => entry != null)
  const folders = entries.filter((entry): entry is FileSystemDirectoryEntry => entry.isDirectory)
  if (folders.length === 0) return pickedFiles(transfer.files)
  const files: { path: string; file: File }[] = []
  for (const folder of folders) files.push(...(await walk(folder)))
  return folderPicked(files)
}

async function walk(dir: FileSystemDirectoryEntry): Promise<{ path: string; file: File }[]> {
  const out: { path: string; file: File }[] = []
  const reader = dir.createReader()
  // A reader gives a folder's entries a batch at a time, then none.
  for (;;) {
    const batch = await new Promise<FileSystemEntry[]>((resolve, reject) => reader.readEntries(resolve, reject))
    if (batch.length === 0) return out
    for (const entry of batch) {
      if (entry.isDirectory) {
        out.push(...(await walk(entry as FileSystemDirectoryEntry)))
      } else {
        const file = await new Promise<File>((resolve, reject) => (entry as FileSystemFileEntry).file(resolve, reject))
        out.push({ path: entry.fullPath.replace(/^\//, ''), file })
      }
    }
  }
}
