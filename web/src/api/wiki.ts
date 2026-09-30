import { apiBase } from './base'
import { keepPreviousData, useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { agentKeys } from './agents'
import { api } from './client'
import { refresh } from './refresh'
import { patchQuery } from './live'
import type {
  Agent,
  AgentRef,
  LocalSkill,
  MemoryView,
  SkillUse,
  UploadedSkill,
  WikiCatalogResponse,
  WikiGraph,
  WikiHistoryResponse,
  WikiPage,
  WikiPageResponse,
  WikiProjectHit,
  WikiQuestion,
  WikiSearchResponse,
  WikiSummary,
} from './types'

// The wikis (docs/design.md 5.3 to 5.5, 5.10): each project's, shown in its
// chat's Wiki tab, and the skill library every project shares, on a page of
// its own. The two read the same way, so the UI takes either as a space.
// The personal memory (5.16) is a bundle of one page kept in Settings; its
// changes read and undo like a wiki's.
// A project's wiki is read in its chat's Wiki tab, or, standalone, on the
// Wiki page of the sidebar (5.18), whose addresses it then keeps to.
export type WikiSpace = { kind: 'project'; projectId: string; roomId: string; standalone?: boolean } | { kind: 'library' } | { kind: 'personal' }

// librarySpace is the skill library.
export const librarySpace: WikiSpace = { kind: 'library' }

// MemorySpace is where a memory is kept: a project's wiki, or its own.
export type MemorySpace = Extract<WikiSpace, { kind: 'project' } | { kind: 'personal' }>

// personalSpace is the personal memory.
export const personalSpace: MemorySpace = { kind: 'personal' }

// wikiFileUrl is where a file the project's wiki keeps is read from: a
// picture shown in its page, anything else downloaded (docs/design.md 5.16).
export function wikiFileUrl(projectId: string, path: string): string {
  return `${apiBase}/projects/${projectId}/wiki/file?path=${encodeURIComponent(path)}`
}

// Everything a space shows sits under its key, so a wiki_changed event
// reads it all again at once.
export const wikiKeys = {
  space: (space: WikiSpace) =>
    space.kind === 'library' ? (['library'] as const) : space.kind === 'personal' ? (['memory'] as const) : (['wiki', space.projectId] as const),
  catalog: (space: WikiSpace) => [...wikiKeys.space(space), 'catalog'] as const,
  page: (space: WikiSpace, path: string) => [...wikiKeys.space(space), 'page', path] as const,
  search: (space: WikiSpace, query: string) => [...wikiKeys.space(space), 'search', query] as const,
  history: (space: WikiSpace, path: string) => [...wikiKeys.space(space), 'history', path] as const,
  uses: (name: string) => ['library', 'uses', name] as const,
  memory: (space: MemorySpace) => [...wikiKeys.space(space), 'memory'] as const,
  // Every project's wiki at once (5.18).
  all: ['wikis'] as const,
  allSearch: (query: string) => ['wikis', 'search', query] as const,
}

// base is where a space's API is.
function base(space: WikiSpace): string {
  return space.kind === 'library' ? '/library' : space.kind === 'personal' ? '/memory' : `/projects/${space.projectId}/wiki`
}

const ready = (space: WikiSpace) => space.kind !== 'project' || space.projectId !== ''

const query = (params: Record<string, string>) => new URLSearchParams(params).toString()

// A wiki read with no chat to tell of its changes looks again now and then:
// the library, and a project's read standalone.
const unwatched = 15_000
const refetchUnwatched = (space: WikiSpace) => (space.kind === 'library' || (space.kind === 'project' && space.standalone) ? unwatched : false)

export function useWikiCatalog(space: WikiSpace) {
  return useQuery({
    queryKey: wikiKeys.catalog(space),
    queryFn: async () => (await api.get<WikiCatalogResponse>(base(space))).wiki,
    enabled: ready(space),
    refetchInterval: refetchUnwatched(space),
  })
}

export function useWikiPage(space: WikiSpace, path: string) {
  return useQuery({
    queryKey: wikiKeys.page(space, path),
    queryFn: async () => (await api.get<WikiPageResponse>(`${base(space)}/page?${query({ path })}`)).page,
    enabled: ready(space) && path !== '',
  })
}

// Searching keeps the last results on screen while the next arrive, so the
// list does not flash empty between keystrokes.
export function useWikiSearch(space: WikiSpace, text: string) {
  const q = text.trim()
  return useQuery({
    queryKey: wikiKeys.search(space, q),
    queryFn: async () => (await api.get<WikiSearchResponse>(`${base(space)}/search?${query({ q, limit: '50' })}`)).hits,
    enabled: ready(space) && q !== '',
    placeholderData: keepPreviousData,
  })
}

// The latest changes to one page, or with no path to the whole wiki.
export function useWikiHistory(space: WikiSpace, path = '', limit = 50, enabled = true) {
  return useQuery({
    queryKey: [...wikiKeys.history(space, path), limit],
    queryFn: async () => (await api.get<WikiHistoryResponse>(`${base(space)}/history?${query({ path, limit: String(limit) })}`)).commits,
    enabled: enabled && ready(space),
    placeholderData: keepPreviousData,
  })
}

// A wiki as a graph (docs/design.md 5.17); the personal memory has none.
export function useWikiGraph(space: WikiSpace) {
  return useQuery({
    queryKey: [...wikiKeys.space(space), 'graph'],
    queryFn: async () => (await api.get<{ graph: WikiGraph }>(`${base(space)}/graph`)).graph,
    // The personal memory is one page, with nothing to relate.
    enabled: ready(space) && space.kind !== 'personal',
    refetchInterval: refetchUnwatched(space),
  })
}

// The turns that used a skill, newest first.
// The turns that used a skill read at most: a list this long may go on.
export const skillUsesLimit = 50

export function useSkillUses(name: string) {
  return useQuery({
    queryKey: wikiKeys.uses(name),
    queryFn: async () => (await api.get<{ uses: SkillUse[] }>(`/library/usage?${query({ name, limit: String(skillUsesLimit) })}`)).uses,
    enabled: name !== '',
  })
}

// skillPath is the page of the library's skill name.
export function skillPath(name: string): string {
  return `/skills/${name}/SKILL.md`
}

// skillExportUrl downloads a skill of the library as a zip file of its
// folder in Agent Skills form, to take elsewhere (docs/design.md 5.15).
export function skillExportUrl(name: string): string {
  return `${apiBase}/library/export?name=${encodeURIComponent(name)}`
}

// skillName is the skill a page of the library is the SKILL.md of; '' for
// any other page.
export function skillName(path: string): string {
  return /^\/skills\/([^/]+)\/SKILL\.md$/.exec(path)?.[1] ?? ''
}

// Installing a skill of the library for an agent, or taking it off
// (docs/design.md 5.15). The skill's page and the agent both show it.
export function useInstallSkill() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async ({ name, agentId, installed }: { name: string; agentId: string; installed: boolean }) =>
      (await api.post<{ installed: AgentRef[] }>('/library/install', { name, agent_id: agentId, installed })).installed,
    onSuccess: (installed, { name, agentId, installed: on }) => {
      patchQuery<WikiPage>(client, wikiKeys.page(librarySpace, skillPath(name)), (old) => (old ? { ...old, installed } : old))
      patchQuery<Agent[]>(client, agentKeys.all, (old) =>
        old?.map((agent) =>
          agent.id !== agentId ? agent : { ...agent, skills: on ? [...new Set([...agent.skills, name])] : agent.skills.filter((s) => s !== name) },
        ),
      )
    },
  })
}

// Rolling a skill on trial back to the version before an agent changed
// it, for a reason that may be empty (docs/design.md 5.15).
export function useRollbackSkill() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async ({ name, reason }: { name: string; reason: string }) => (await api.post<WikiPageResponse>('/library/rollback', { name, reason })).page,
    onSuccess: (page) => {
      client.setQueryData<WikiPage>(wikiKeys.page(librarySpace, page.path), page)
      invalidateSpace(client, librarySpace)
    },
  })
}

// Adding a skill to the library from a folder on the machine Veyloom runs
// on, looked after by a project's team or by none ('').
export function useImportSkill() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async ({ folder, projectId }: { folder: string; projectId: string }) =>
      (await api.post<WikiPageResponse>('/library/import', { folder, project_id: projectId })).page,
    onSuccess: (page) => {
      client.setQueryData<WikiPage>(wikiKeys.page(librarySpace, page.path), page)
      invalidateSpace(client, librarySpace)
    },
  })
}

// The skills on this machine a person may bring into the library, read
// when the import opens.
export function useLocalSkills() {
  return useQuery({
    queryKey: ['library', 'local'],
    queryFn: async () => (await api.get<{ skills: LocalSkill[] }>('/library/local')).skills,
    staleTime: 0,
  })
}

// What a person sends from the browser: a zip file, or the files of a
// folder, each with its path in the folder, which starts with its name.
export type SkillUploadBody = { zip: File } | { files: { path: string; file: File }[] }

// Importing the skills an upload holds; how each went comes back.
export function useUploadSkills() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async ({ upload, projectId }: { upload: SkillUploadBody; projectId: string }) => {
      const form = new FormData()
      if (projectId) form.append('project_id', projectId)
      if ('zip' in upload) {
        form.append('file', upload.zip, upload.zip.name)
      } else {
        for (const { path, file } of upload.files) {
          form.append('path', path)
          form.append('file', file, file.name)
        }
      }
      return (await api.upload<{ skills: UploadedSkill[] }>('/library/upload', form)).skills
    },
    onSuccess: () => invalidateSpace(client, librarySpace),
  })
}

// A person's change to a page: confirming it, making it resident (a
// project's wiki only), or handing a skill to another team (the library).
export type PageChange = { path: string } & ({ verify: true } | { resident: boolean } | { team: string })

export function useChangeWikiPage(space: WikiSpace) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (change: PageChange) => {
      if ('team' in change) {
        const name = change.path.split('/')[2] ?? ''
        return (await api.post<WikiPageResponse>('/library/transfer', { name, project_id: change.team })).page
      }
      const action = 'verify' in change ? 'verify' : 'resident'
      return (await api.post<WikiPageResponse>(`${base(space)}/${action}`, change)).page
    },
    onSuccess: (page) => {
      client.setQueryData<WikiPage>(wikiKeys.page(space, page.path), page)
      invalidateSpace(client, space)
    },
  })
}

// Asking about a page of a project's wiki: the wiki topic, opened if it
// was not, and whom to ask there.
export function useWikiQuestion(projectId: string) {
  return useMutation({
    mutationFn: async () => (await api.post<{ question: WikiQuestion }>(`/projects/${projectId}/wiki/question`, {})).question,
  })
}

export function useRevertWiki(space: WikiSpace) {
  const client = useQueryClient()
  return useMutation({
    // The reason, which may be empty, goes into the wiki's log.
    mutationFn: async ({ sha, reason }: { sha: string; reason: string }) =>
      (await api.post<{ commit: string }>(`${base(space)}/revert`, { sha, reason })).commit,
    onSuccess: () => invalidateSpace(client, space),
  })
}

// memoryPath is where a memory is read and saved.
function memoryPath(space: MemorySpace): string {
  return space.kind === 'personal' ? '/memory' : `/projects/${space.projectId}/memory`
}

export function useMemory(space: MemorySpace) {
  return useQuery({
    queryKey: wikiKeys.memory(space),
    queryFn: async () => (await api.get<{ memory: MemoryView }>(memoryPath(space))).memory,
    enabled: ready(space),
  })
}

// Saving a memory as the person left it, from the hash it was read at:
// entries left as they were keep their day and source. A memory changed
// in the meantime is read again, for the person to try once more.
export function useSaveMemory(space: MemorySpace) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async ({ entries, hash }: { entries: string[]; hash: string }) =>
      (await api.put<{ memory: MemoryView }>(memoryPath(space), { entries, hash })).memory,
    onSuccess: (memory) => {
      client.setQueryData<MemoryView>(wikiKeys.memory(space), memory)
      invalidateSpace(client, space)
    },
    onError: () => void client.invalidateQueries({ queryKey: wikiKeys.memory(space) }),
  })
}

// invalidateSpace reads everything a wiki shows again.
export function invalidateSpace(client: QueryClient, space: WikiSpace) {
  void client.invalidateQueries({ queryKey: wikiKeys.space(space) })
}

// invalidateWiki reads everything a project's wiki shows again; no project
// stands for the library.
export function invalidateWiki(client: QueryClient, projectId: string) {
  refresh(client, { queryKey: projectId === '' ? ['library'] : ['wiki', projectId] })
}

// Every project's wiki, a line about each (docs/design.md 5.18).
export function useWikis() {
  return useQuery({
    queryKey: wikiKeys.all,
    queryFn: async () => (await api.get<{ wikis: WikiSummary[] }>('/wikis')).wikis,
    refetchInterval: unwatched,
  })
}

// A search through every project's wiki, best first.
export function useWikisSearch(q: string) {
  return useQuery({
    queryKey: wikiKeys.allSearch(q),
    queryFn: async () => (await api.get<{ hits: WikiProjectHit[] }>(`/wikis/search?${query({ q })}`)).hits,
    enabled: q !== '',
    placeholderData: keepPreviousData,
  })
}
