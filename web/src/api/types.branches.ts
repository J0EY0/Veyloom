// Wire types for a project's branches (docs/design.md 5.21); see types.ts.

// A file changed: in a worktree since it branched off, or in the checkout
// and not committed. Status is A added, M modified, D deleted, R renamed
// (from), T its type changed.
export interface WorktreeChange {
  path: string
  status: 'A' | 'M' | 'D' | 'R' | 'T'
  from?: string
  added: number
  deleted: number
  binary?: boolean
  uncommitted?: boolean
  // In no commit at all, made since and not committed: a merge may leave
  // it out, and it stays in the worktree.
  new?: boolean
}

// How a member's worktree stands against the main line.
export interface WorktreeStatus {
  branch: string
  base?: string
  ahead: number
  behind: number
  files?: WorktreeChange[]
  uncommitted: number
  // A merge under way, stopped on conflicts, and the files that still have
  // conflict markers.
  merging?: boolean
  conflicts?: string[]
  // Its own commits the main line lacks, oldest first, the latest few.
  commits?: WorktreeCommit[]
}

// A commit a member made in its worktree, by what its message says.
export interface WorktreeCommit {
  hash: string
  subject: string
  body?: string
}

// The project's checkout: the main line is the branch it has.
export interface MainLine {
  repo_path: string
  // In a git repository with a commit: only then do members get worktrees.
  git: boolean
  branch?: string
  changed?: WorktreeChange[]
  error?: string
}

// A member that has a worktree, or will have one the first time it works.
export interface MemberBranch {
  member_id: string
  name: string
  branch?: string
  dir?: string
  prepared: boolean
  busy: boolean
  status?: WorktreeStatus
  error?: string
  // A first line for the commit that merges its work: what its own
  // commits say, or else what it was last asked.
  draft?: string
  // The members whose work its branch has in full, having merged theirs.
  contains?: string[]
}

// A file more than one member changed, by member id.
export interface Overlap {
  path: string
  members: string[]
}

export interface Branches {
  main: MainLine
  members: MemberBranch[]
  overlaps?: Overlap[]
}

export interface BranchesResponse {
  branches: Branches
}

export interface DiffResponse {
  patch: string
  cut?: boolean
}

export interface MergeResponse {
  // The new commit, or the files that conflict and nothing changed;
  // unsettled says why the worktree did not start over after a merge.
  merge: { commit?: string; conflicts?: string[]; unsettled?: string }
}

// Where a member's work set aside is kept: a ref of the repository's.
export interface SetAsideResponse {
  ref: string
}

export interface SyncResponse {
  sync: { updated?: boolean; skipped?: boolean; conflicts?: string[] }
}

export interface CommitResponse {
  commit: string
}
