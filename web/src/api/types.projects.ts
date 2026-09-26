// Wire types for projects; see types.ts.

import type { UpkeepTrigger } from './types.wiki'

export interface Project {
  id: string
  name: string
  // Where the code is checked out on the machines its members run on; the
  // members a project starts with work there.
  repo_path: string
  // What the project is, in a paragraph; every agent's brief opens with it.
  // Always sent by the API; optional here until the settings UI uses it.
  description?: string
  // The project's group chat: a project is one chat (docs/webui.md §3).
  main_room_id: string
  // The project's wiki topic in its chat, once there is one: where its
  // wiki maintainer runs.
  wiki_thread_id?: string
  // The member a person made the project's leader (docs/design.md 5.21),
  // absent when none was; and the leader as it stands, either way: the
  // first current, enabled member to have joined when none was made it.
  leader_member_id?: string
  leader_id?: string
  // Whether the wiki is kept (docs/design.md 5.12, 5.21); by the member a
  // person chose, or by the leader when none was; and when.
  wiki_upkeep?: boolean
  wiki_maintainer_member_id?: string
  wiki_maintainer_trigger?: UpkeepTrigger
  // The note in the chat that offered a maintainer to a project without
  // one, drawn as a card, and when a person said no to it (docs/design.md
  // 5.16).
  wiki_offer_message_id?: string
  wiki_offer_declined_at?: string
  // The folders of the OKF bundles the wiki mounts, read-only.
  wiki_external_bundles?: string[]
  // How a member's new git worktree is got ready (docs/design.md 5.21);
  // steps the leader wrote down that wait for a person to adopt, with the
  // note in the chat that shows them; when the project was set up for
  // worktrees; and its setup topic.
  workspace_copy?: string[]
  workspace_run?: string
  workspace_pending?: WorkspaceSteps
  workspace_pending_message_id?: string
  initialized_at?: string
  setup_thread_id?: string
  // How many turns agents may wake one another to in a piece of work a
  // person started (docs/design.md 5.22); 0 is no limit.
  relay_limit?: number
  created_at: string
}

// How a new worktree is got ready: what is copied into it from the
// checkout, then one command run in it.
export interface WorkspaceSteps {
  copy: string[]
  run: string
}

export interface CreateProjectRequest {
  name: string
  repo_path?: string
  // What the project is, in a paragraph; every agent's brief opens with it.
  description?: string
  // The agents that join the chat as its first members.
  agent_ids: string[]
  // The wiki kept from the start, daily unless said otherwise, by one of
  // them or, without one, by the leader; off, the chat offers it later.
  wiki_upkeep?: boolean
  wiki_maintainer_agent_id?: string
  wiki_maintainer_trigger?: UpkeepTrigger
}

// An absent field keeps its value. Moving the checkout moves the project's
// current members with it: they work under it.
export interface UpdateProjectRequest {
  name?: string
  repo_path?: string
  description?: string
  // "" leaves the lead to the first member to have joined.
  leader_member_id?: string
  wiki_upkeep?: boolean
  // "" leaves the wiki to the leader.
  wiki_maintainer_member_id?: string
  wiki_maintainer_trigger?: UpkeepTrigger
  // All of them: an empty list mounts none.
  wiki_external_bundles?: string[]
  // A person said no to the maintainer the chat offered.
  wiki_offer_declined?: boolean
  // Written by a person: the project is set up.
  workspace_steps?: WorkspaceSteps
  relay_limit?: number
}
