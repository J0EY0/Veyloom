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

// folderPicked leaves out hidden files, a Mac's .DS_Store among them,
// which the import leaves behind anyway.
function folderPicked(files: { path: string; file: File }[]): Picked | undefined {
  const kept = files.filter(({ path }) => !path.split('/').some((part) => part.startsWith('.')))
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
