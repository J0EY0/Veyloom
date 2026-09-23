// The project wiki as the API serves it (internal/hub/wikiview.go,
// docs/design.md 5.3 to 5.5). A page is named by its path from the wiki's
// root, like /decisions/payload-json.md.

import type { Turn } from './types'

export type WikiStatus = 'draft' | 'stable' | 'deprecated'
export type WikiTier = 'unverified' | 'machine-confirmed' | 'human-reviewed'

// A page as lists show it.
export interface WikiPageInfo {
  path: string
  type: string
  title: string
  description?: string
  tags: string[]
  status: WikiStatus
  tier: WikiTier
  // Who last wrote the content, an OKF actor like codex/default or
  // human:alice, and when.
  generated_by?: string
  generated_at?: string
  verified_at?: string
  // When a person last stood behind the page as it is: wrote it, or
  // confirmed it since it was last written.
  vouched_at?: string
  stale_after?: string
  stale?: boolean
  modified: string
  // Every turn carries the page in full: tagged resident, and vouched for.
  resident: boolean
  // The project owning a skill, by its wiki folder name; none for other
  // pages and for a skill nobody owns.
  team?: string
  // A skill of the library on trial after an agent changed it.
  on_trial?: boolean
  // The bundle the project mounts that the page is in; such a page is
  // read-only. Absent for the wiki's own pages.
  mount?: string
  // When the page was last written or confirmed, whichever is later, and
  // why it is due to be checked again, when it is (docs/design.md 5.16).
  // A project's own pages only.
  checked_at?: string
  review?: WikiReview
}

// Why a page is due to be checked again: a file it names changed since, as
// long passed as pages like it hold, or its stale_after came.
export interface WikiReview {
  why: 'changed' | 'period' | 'stale'
  file?: string
  turn_id?: string
  room_id?: string
  thread_id?: string
  topic_number?: number
  changed_at?: string
  every?: number
}

export interface WikiDir {
  name: string
  type: string
}

export interface WikiCatalog {
  pages: WikiPageInfo[]
  // The types of page, in the order the wiki lists them.
  dirs: WikiDir[]
  // The wiki's folder on disk.
  folder: string
  // Changes can be looked back on and undone.
  history: boolean
  // The projects owning the library's skills.
  teams?: WikiTeam[]
  // The bundles a project's wiki mounts, read-only (docs/design.md 5.9).
  mounts?: WikiMount[]
}

// One bundle a project's wiki mounts: its pages at the paths Veyloom gives
// them, /@<name>/...
export interface WikiMount {
  name: string
  folder: string
  pages: WikiPageInfo[]
  // Why it cannot be read; there are no pages then. error_code names it
  // for the word lists (problemText).
  error?: string
  error_code?: string
  error_params?: Record<string, string>
}

export interface WikiTeam {
  slug: string
  project_id: string
  name: string
  room_id: string
}

// A turn that used a skill of the library.
export interface SkillUse {
  turn_id: string
  status: string
  runtime: string
  started_at: string
  ended_at?: string
  room_id: string
  thread_id: string
  topic_number: number
  member_name: string
  project_id: string
  project_name: string
}

export interface WikiLink {
  path: string
  title: string
}

export interface WikiStamp {
  by: string
  at: string
}

// What a page rests on, and where it leads: a topic of the chat (and the
// turn in it), another page, or somewhere outside.
export interface WikiSource {
  id?: string
  resource: string
  title?: string
  room_id?: string
  thread_id?: string
  topic_number?: number
  turn_id?: string
  // Or the message a file kept in the wiki came with: who sent it and
  // when, and its topic above unless it was said in the chat itself.
  message_id?: string
  sent_by?: string
  sent_at?: string
  page?: string
}

export interface WikiPage extends WikiPageInfo {
  // The markdown, its links to other pages from the wiki's root.
  body: string
  hash: string
  file: string
  verified: WikiStamp[]
  sources: WikiSource[]
  backlinks: WikiLink[]
  // A skill of the library: the agents it is installed for, and its latest
  // trial, open or how it ended.
  installed?: AgentRef[]
  trial?: SkillTrial
}

export type SkillTrialStatus = 'open' | 'kept' | 'rolled_back'

// A skill on trial after an agent changed it (docs/design.md 5.15): kept
// once enough turns used it and ended well, or when a person confirms it;
// rolled back by a person or the maintainer of its team.
export interface SkillTrial {
  id: string
  skill: string
  base_sha: string
  started_at: string
  // The last change: when, in which turn, by which member of which
  // project, and how many changes the trial has had.
  changed_at: string
  turn_id?: string
  changed_by: string
  project_name: string
  changes: number
  status: SkillTrialStatus
  // Who ended it, an OKF actor, and why when they said.
  ended_by?: string
  ended_at?: string
  reason?: string
  // While open: the turns that used it since the last change, by how they
  // ended, and how many that ended well keep it.
  uses: number
  failed: number
  needed: number
  // Where the last change was made, while its turn is there.
  room_id?: string
  thread_id?: string
  topic_number?: number
}

// An agent by name, as a skill's page lists whom it is installed for.
export interface AgentRef {
  id: string
  name: string
}

export interface WikiHit extends WikiPageInfo {
  snippet?: string
}

// One project's wiki as the Wiki page of the sidebar lists it (docs/design.md
// 5.18): its current pages, the project memory not counted, those due to be
// checked again, and when it last changed.
export interface WikiSummary {
  project_id: string
  project_name: string
  room_id: string
  pages: number
  due: number
  changed_at?: string
}

// A page found searching every project's wiki, with whose it is.
export interface WikiProjectHit extends WikiHit {
  project_id: string
  project_name: string
  room_id: string
}

export interface WikiChangeLine {
  // The log's word: Creation, Update, Deprecation, Rename, Verification,
  // Revert, Rejection, Initialization.
  kind: string
  path?: string
  title?: string
  text: string
}

export interface WikiCommit {
  sha: string
  author: string
  at: string
  subject: string
  changes: WikiChangeLine[]
  // Where it came from, when a turn wrote it.
  member?: string
  // The project that turn was in, for the library's changes.
  project_name?: string
  turn_id?: string
  room_id?: string
  thread_id?: string
  topic_number?: number
  // It changed pages, which undoing it takes back.
  undoable: boolean
}

// Where a person's doubt about a page goes (docs/design.md 5.15): the
// wiki topic, and the member to ask there.
export interface WikiQuestion {
  thread_id: string
  member_id: string
  member_name: string
}

export interface WikiCatalogResponse {
  wiki: WikiCatalog
}

export interface WikiPageResponse {
  page: WikiPage
}

export interface WikiSearchResponse {
  hits: WikiHit[]
}

// A memory every turn carries whole (docs/design.md 5.16): the personal
// one, or a project's. An entry is one line, with the day it was noted and
// who noted it.
export interface MemoryEntry {
  text: string
  date?: string
  source?: string
}

// Which memories the members' turns use (docs/design.md 5.19): the switch
// for memory as a whole, and one for each memory under it. Off only means
// unused; what a memory holds stays.
export interface MemoryPrefs {
  enabled: boolean
  personal: boolean
  project: boolean
}

export interface MemoryView {
  entries: MemoryEntry[]
  // How much of its budget it takes, in characters.
  chars: number
  budget: number
  // What a change is made from; empty while there is no memory.
  hash: string
  file?: string
}

// A wiki as a graph (docs/design.md 5.17): its pages, the paths of the
// repository they name and the topics they came from, and how they bear on
// each other. A page's id is its path; a path of the repository's is
// "file:" and the path; a topic's "topic:" and its thread.
export type GraphNodeKind = 'page' | 'external' | 'file' | 'topic'

export interface GraphTopic {
  room_id: string
  thread_id: string
  number: number
  title: string
}

export interface GraphNode {
  id: string
  kind: GraphNodeKind
  // A page's, a mounted one's too.
  page?: WikiPageInfo
  // A path of the repository: the fullest way pages write it, whether
  // other paths sit in it, and how many pages name it.
  file?: string
  dir?: boolean
  pages?: number
  topic?: GraphTopic
}

// A link carries the sentence it sits in, which says what the relation is;
// a source the source's title.
export type GraphEdgeKind = 'link' | 'supersedes' | 'source' | 'names' | 'from' | 'contains'

export interface GraphEdge {
  from: string
  to: string
  kind: GraphEdgeKind
  context?: string
}

export interface WikiGraph {
  nodes: GraphNode[]
  edges: GraphEdge[]
}

export interface WikiHistoryResponse {
  commits: WikiCommit[]
}

// When a project's wiki maintainer goes over what the chat did: once a
// topic has been quiet a while, once a day, or when a person asks.
// When a project's wiki maintainer goes over the chat: daily by default, a
// week at the longest (docs/design.md 5.16).
export type UpkeepTrigger = 'idle' | 'daily' | 'every_3_days' | 'weekly' | 'manual'

// How a project's wiki upkeep stands (GET /projects/{id}/wiki/maintainer).
export interface UpkeepStatus {
  // The maintainer; absent when there is none.
  member_id?: string
  member_name?: string
  trigger: UpkeepTrigger
  // How long a topic stays quiet before an upkeep on quiet topics.
  idle_minutes: number
  // What the next upkeep would go over: this chat's turns, other projects'
  // turns with the team's skills, and how many of all ran in quiet topics.
  // What people said since the last upkeep counts too (docs/design.md 5.16).
  waiting: { own: number; uses: number; settled: number; people?: number; people_settled?: number }
  // An upkeep waits for its member to be free.
  queued: boolean
  // The latest upkeep, running or not.
  last?: Turn
  // The wiki topic, where upkeeps run.
  thread_id?: string
}

export interface UpkeepResponse {
  upkeep: UpkeepStatus
}
