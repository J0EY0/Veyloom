// Package api exposes the hub over HTTP.
//
// Everything is JSON and versioned under /api/v1 so later additions do not
// break clients. Handlers depend on small interfaces so tests can substitute
// in-memory fakes; the real implementations are *machine.Discovery,
// *hub.Hub and *store.Store.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/J0EY0/veyloom/internal/hub"
	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/worktree"
)

// Discoverer runs runtime discovery on this machine.
type Discoverer interface {
	Run(ctx context.Context) []runtime.Info
}

// MachineRegistry reports the machines connected to the hub and asks one to
// discover its runtimes again.
type MachineRegistry interface {
	Machines() []hub.MachineInfo
	Probe(ctx context.Context, machineID string) error
}

// ProjectStore persists projects and their rooms.
type ProjectStore interface {
	CreateProject(ctx context.Context, p store.NewProject) (store.Project, store.Room, error)
	GetProject(ctx context.Context, id string) (store.Project, error)
	ListProjects(ctx context.Context) ([]store.Project, error)
	UpdateProject(ctx context.Context, id string, patch store.ProjectPatch) (store.Project, error)
	DeleteProject(ctx context.Context, id string) (store.ProjectRemains, error)
	CreateRoom(ctx context.Context, projectID, name string) (store.Room, error)
	GetRoom(ctx context.Context, id string) (store.Room, error)
	ListRooms(ctx context.Context, projectID string) ([]store.Room, error)
}

// Worktrees is what the API asks of the members' git worktrees
// (docs/design.md 5.21); *hub.Hub.
type Worktrees interface {
	// ReleaseWorktree takes a removed member's worktree away, its work kept
	// on its branch.
	ReleaseWorktree(member store.Member)
	Branches(ctx context.Context, projectID string) (hub.Branches, error)
	DiffOf(ctx context.Context, memberID string) (patch string, cut bool, err error)
	Merge(ctx context.Context, memberID, message string, leave []string) (worktree.MergeResult, error)
	SyncMember(ctx context.Context, memberID string) (worktree.SyncResult, error)
	// SetAside gives up what a member's worktree has, kept in git under
	// the ref it returns, and starts the worktree over.
	SetAside(ctx context.Context, memberID string) (string, error)
	// AbortMerge gives up a merge a member left under way in its worktree.
	AbortMerge(ctx context.Context, memberID string) error
	// CheckoutDiff is the patch of what the project's checkout changed and
	// did not commit; CommitCheckout commits the changes to the files a
	// person picked there.
	CheckoutDiff(ctx context.Context, projectID string) (patch string, cut bool, err error)
	CommitCheckout(ctx context.Context, projectID, message string, paths []string) (string, error)
	// StartSetup has the leader set the project up again;
	// SettleWorkspaceSteps adopts the steps it wrote down, or turns them
	// down; WorkspaceStepsWritten says a person wrote them.
	StartSetup(ctx context.Context, projectID string) error
	SettleWorkspaceSteps(ctx context.Context, projectID, userID string, adopt bool) error
	WorkspaceStepsWritten(projectID string)
}

// Wikis is what the API asks of the project wikis; *hub.Hub.
type Wikis interface {
	// ArchiveProjectWiki puts a deleted project's wiki aside.
	ArchiveProjectWiki(projectID, slug string) error
	WikiCatalog(ctx context.Context, projectID string) (hub.WikiCatalog, error)
	WikiPage(ctx context.Context, projectID, path string) (hub.WikiPageView, error)
	// A file the wiki keeps beside its pages (docs/design.md 5.16).
	WikiFile(ctx context.Context, projectID, path string) ([]byte, error)
	SearchWiki(ctx context.Context, projectID, query string, limit int) ([]hub.WikiHit, error)
	WikiHistory(ctx context.Context, projectID, path string, limit int) ([]hub.WikiCommit, error)
	RevertWiki(ctx context.Context, projectID, sha, userID, reason string) (string, error)
	VerifyWikiPage(ctx context.Context, projectID, path, userID string) (hub.WikiPageView, error)
	SetWikiResident(ctx context.Context, projectID, path string, resident bool, userID string) (hub.WikiPageView, error)
	// Where a person's doubt about a page goes (docs/design.md 5.15).
	WikiQuestion(ctx context.Context, projectID string) (hub.WikiQuestion, error)
	// The wikis as graphs (docs/design.md 5.17).
	WikiGraph(ctx context.Context, projectID string) (hub.WikiGraph, error)
	LibraryGraph(ctx context.Context) (hub.WikiGraph, error)
	// Every project's wiki at once (docs/design.md 5.18).
	Wikis(ctx context.Context) ([]hub.WikiSummary, error)
	SearchWikis(ctx context.Context, query string, limit int) ([]hub.WikiProjectHit, error)

	// The skill library every project shares (docs/design.md 5.10).
	LibraryCatalog(ctx context.Context) (hub.WikiCatalog, error)
	LibraryPage(ctx context.Context, path string) (hub.WikiPageView, error)
	SearchLibrary(ctx context.Context, query string, limit int) ([]hub.WikiHit, error)
	LibraryHistory(ctx context.Context, path string, limit int) ([]hub.WikiCommit, error)
	RevertLibrary(ctx context.Context, sha, userID, reason string) (string, error)
	VerifyLibraryPage(ctx context.Context, path, userID string) (hub.WikiPageView, error)
	TransferSkill(ctx context.Context, name, projectID, userID string) (hub.WikiPageView, error)
	SkillUses(ctx context.Context, name string, limit int) ([]store.SkillUse, error)
	// People add skills to the library and install them for agents
	// (docs/design.md 5.15).
	ImportSkill(ctx context.Context, folder, team, userID string) (hub.WikiPageView, error)
	InstallSkill(ctx context.Context, name, agentID string, installed bool) ([]store.AgentRef, error)
	CheckSkills(ctx context.Context, names []string) error
	// BuiltinSkills are Veyloom's own, which every agent has
	// (docs/design.md 5.23.6).
	BuiltinSkills() []hub.BuiltinSkill
	// A person rolls back an agent's change to a skill on trial.
	RollbackSkill(ctx context.Context, name, userID, reason string) (hub.WikiPageView, error)

	// The project's wiki maintainer (docs/design.md 5.12).
	UpkeepStatus(ctx context.Context, projectID string) (hub.UpkeepStatus, error)
	StartUpkeep(ctx context.Context, projectID string) (hub.UpkeepStatus, error)

	// The memories every turn carries (docs/design.md 5.16): a project's,
	// or the personal one when projectID is empty.
	Memory(ctx context.Context, projectID string) (hub.MemoryView, error)
	SetMemory(ctx context.Context, projectID string, entries []string, hash, userID string) (hub.MemoryView, error)
	MemoryHistory(ctx context.Context, limit int) ([]hub.WikiCommit, error)
	RevertMemory(ctx context.Context, sha, userID, reason string) (string, error)
}

// Deps are the collaborators the handlers need.
type Deps struct {
	Runtimes Discoverer
	Machines MachineRegistry
	Projects ProjectStore
	Users    UserStore
	// Auth signs the person in; nil leaves every route open (tests).
	Auth      Authenticator
	Messages  MessageStore
	Agents    AgentStore
	Turns     TurnStore
	Approvals ApprovalStore
	// Rules keeps what people allowed members always; nil keeps none.
	Rules RuleStore
	// Work reads the task board, pieces of work and usage; nil reads none.
	Work WorkStore
	// Chat posts user messages, controls turns, settles approvals and
	// streams room events; it is the hub.
	Chat Chat
	// Events tunes the WebSocket event stream.
	Events EventsOptions
	// TranscriptDir is where the hub writes turn transcripts; the transcript
	// endpoint reads a running turn's file from there.
	TranscriptDir string
	// Attachments records uploads; AttachmentDir is where their bytes go.
	// Both empty means uploads are refused.
	Attachments   AttachmentStore
	AttachmentDir string
	// AvatarDir is where the pictures uploaded for agents go. Empty means
	// avatars are refused.
	AvatarDir string
	// Wikis keeps the project wikis. Nil means the API leaves them alone.
	Wikis Wikis
	// Prefs keeps the account's memory switches (docs/design.md 5.19).
	// Nil means there are none to change.
	Prefs MemoryPrefsStore
	// Worktrees looks after the members' git worktrees (docs/design.md
	// 5.21). Nil leaves them be.
	Worktrees Worktrees
	// Logger receives unexpected errors. nil means slog.Default().
	Logger *slog.Logger
	// Now is the clock wrong passwords are counted by; nil means
	// time.Now.
	Now func() time.Time
}

// RuntimesResponse is the body of GET /api/v1/runtimes: the runtimes found on
// the machine serving the request.
type RuntimesResponse struct {
	Runtimes []runtime.Info `json:"runtimes"`
}

// RuntimeTraitsResponse is the body of GET /api/v1/runtime-traits: how
// each runtime there is a runner for takes its turns, by name
// (docs/design.md 5.23.9). The web client asks once; nothing is probed.
type RuntimeTraitsResponse struct {
	Traits map[string]runtime.Traits `json:"traits"`
}

// MachinesResponse is the body of GET /api/v1/machines: every machine connected
// to the hub, with the runtimes each one offers.
type MachinesResponse struct {
	Machines []hub.MachineInfo `json:"machines"`
}

// handlers groups the HTTP handlers around their dependencies.
type handlers struct {
	deps Deps
	// signIns counts wrong passwords (signin.go).
	signIns *signInLimiter
}

// NewHandler builds the HTTP handler for the hub API.
func NewHandler(deps Deps) http.Handler {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	h := &handlers{deps: deps, signIns: newSignInLimiter(deps.Now)}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/runtimes", h.listRuntimes)
	mux.HandleFunc("GET /api/v1/runtime-traits", h.runtimeTraits)
	mux.HandleFunc("GET /api/v1/machines", h.listMachines)
	mux.HandleFunc("POST /api/v1/machines/{id}/probe", h.probeMachine)
	mux.HandleFunc("GET /api/v1/machines/{id}/members", h.listMachineMembers)
	mux.HandleFunc("GET /api/v1/machines/{id}/activity", h.machineActivity)

	mux.HandleFunc("POST /api/v1/projects", h.createProject)
	mux.HandleFunc("GET /api/v1/projects", h.listProjects)
	mux.HandleFunc("GET /api/v1/projects/{id}", h.getProject)
	mux.HandleFunc("PATCH /api/v1/projects/{id}", h.updateProject)
	mux.HandleFunc("DELETE /api/v1/projects/{id}", h.deleteProject)
	mux.HandleFunc("POST /api/v1/projects/{id}/rooms", h.createRoom)
	mux.HandleFunc("GET /api/v1/projects/{id}/rooms", h.listRooms)
	mux.HandleFunc("GET /api/v1/rooms/{id}", h.getRoom)

	mux.HandleFunc("GET /api/v1/auth/status", h.authStatus)
	mux.HandleFunc("POST /api/v1/auth/setup", h.authSetup)
	mux.HandleFunc("POST /api/v1/auth/login", h.authLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", h.authLogout)
	mux.HandleFunc("GET /api/v1/me", h.me)
	mux.HandleFunc("PATCH /api/v1/me", h.renameMe)
	mux.HandleFunc("GET /api/v1/settings/memory", h.memoryPrefs)
	mux.HandleFunc("PUT /api/v1/settings/memory", h.setMemoryPrefs)
	mux.HandleFunc("POST /api/v1/me/password", h.changePassword)
	mux.HandleFunc("POST /api/v1/users", h.createUser)
	mux.HandleFunc("GET /api/v1/users", h.listUsers)
	mux.HandleFunc("GET /api/v1/users/{id}", h.getUser)
	mux.HandleFunc("GET /api/v1/users/{id}/inbox", h.userInbox)
	mux.HandleFunc("GET /api/v1/users/{id}/inbox/events", h.inboxEvents)
	mux.HandleFunc("POST /api/v1/users/{id}/inbox/read", h.readInbox)

	mux.HandleFunc("POST /api/v1/rooms/{id}/messages", h.postMessage)
	mux.HandleFunc("GET /api/v1/rooms/{id}/messages", h.listRoomMessages)
	mux.HandleFunc("GET /api/v1/messages/{id}", h.getMessage)
	mux.HandleFunc("GET /api/v1/threads/{id}", h.getThread)
	mux.HandleFunc("GET /api/v1/threads/{id}/messages", h.listThreadMessages)
	mux.HandleFunc("POST /api/v1/rooms/{id}/attachments", h.uploadAttachment)
	mux.HandleFunc("GET /api/v1/attachments/{id}", h.getAttachment)
	mux.HandleFunc("GET /api/v1/attachments/{id}/thumbnail", h.getAttachmentThumbnail)
	mux.HandleFunc("GET /api/v1/rooms/{id}/attachments", h.listRoomAttachments)
	mux.HandleFunc("GET /api/v1/rooms/{id}/addressee", h.addressee)
	mux.HandleFunc("GET /api/v1/rooms/{id}/attachments/archive", h.downloadRoomAttachments)
	mux.HandleFunc("POST /api/v1/avatars", h.uploadAvatar)
	mux.HandleFunc("GET /api/v1/avatars/{name}", h.getAvatar)

	mux.HandleFunc("POST /api/v1/agents", h.createAgent)
	mux.HandleFunc("GET /api/v1/agents", h.listAgents)
	mux.HandleFunc("GET /api/v1/agents/{id}", h.getAgent)
	mux.HandleFunc("PUT /api/v1/agents/{id}", h.updateAgent)
	mux.HandleFunc("DELETE /api/v1/agents/{id}", h.deleteAgent)
	mux.HandleFunc("POST /api/v1/rooms/{id}/members", h.createMember)
	mux.HandleFunc("GET /api/v1/rooms/{id}/members", h.listRoomMembers)
	mux.HandleFunc("GET /api/v1/members/{id}", h.getMember)
	mux.HandleFunc("PATCH /api/v1/members/{id}", h.updateMember)
	mux.HandleFunc("DELETE /api/v1/members/{id}", h.removeMember)
	mux.HandleFunc("GET /api/v1/members/{id}/session", h.getMemberSession)
	mux.HandleFunc("DELETE /api/v1/members/{id}/session", h.resetMemberSession)
	mux.HandleFunc("GET /api/v1/members/{id}/rules", h.listMemberRules)
	mux.HandleFunc("DELETE /api/v1/members/{id}/rules/{rule}", h.deleteMemberRule)

	mux.HandleFunc("GET /api/v1/turns/{id}", h.getTurn)
	mux.HandleFunc("POST /api/v1/turns/{id}/cancel", h.cancelTurn)
	mux.HandleFunc("DELETE /api/v1/turns/{id}/trust", h.untrustTurn)
	mux.HandleFunc("POST /api/v1/relays/{id}/continue", h.continueRelay)
	mux.HandleFunc("GET /api/v1/pauses", h.listPauses)
	mux.HandleFunc("DELETE /api/v1/pauses/{id}", h.liftPause)
	mux.HandleFunc("GET /api/v1/threads/{id}/relay-holds", h.threadRelayHolds)
	mux.HandleFunc("GET /api/v1/threads/{id}/reminders", h.threadReminders)
	mux.HandleFunc("DELETE /api/v1/reminders/{id}", h.cancelReminder)
	mux.HandleFunc("GET /api/v1/threads/{id}/drafts", h.threadDrafts)
	mux.HandleFunc("POST /api/v1/drafts/{id}/run", h.runDraft)
	mux.HandleFunc("POST /api/v1/drafts/{id}/decline", h.declineDraft)
	mux.HandleFunc("GET /api/v1/turns/{id}/transcript", h.turnTranscript)
	mux.HandleFunc("GET /api/v1/rooms/{id}/turns", h.listRoomTurns)
	mux.HandleFunc("GET /api/v1/topics", h.listTopics)
	mux.HandleFunc("GET /api/v1/rooms/{id}/tasks", h.listRoomTasks)
	mux.HandleFunc("GET /api/v1/works/{chain}", h.getWork)
	mux.HandleFunc("GET /api/v1/usage", h.usage)

	mux.HandleFunc("GET /api/v1/approvals", h.listApprovals)
	mux.HandleFunc("GET /api/v1/approvals/{id}", h.getApproval)
	mux.HandleFunc("POST /api/v1/approvals/{id}/decide", h.decideApproval)
	mux.HandleFunc("GET /api/v1/rooms/{id}/approvals", h.listRoomApprovals)
	mux.HandleFunc("GET /api/v1/turns/{id}/approvals", h.listTurnApprovals)

	mux.HandleFunc("GET /api/v1/rooms/{id}/events", h.roomEvents)

	mux.HandleFunc("GET /api/v1/projects/{id}/wiki", h.wikiCatalog)
	mux.HandleFunc("GET /api/v1/projects/{id}/wiki/page", h.wikiPage)
	mux.HandleFunc("GET /api/v1/projects/{id}/wiki/file", h.wikiFile)
	mux.HandleFunc("GET /api/v1/projects/{id}/wiki/search", h.searchWiki)
	mux.HandleFunc("GET /api/v1/projects/{id}/wiki/history", h.wikiHistory)
	mux.HandleFunc("GET /api/v1/projects/{id}/wiki/graph", h.wikiGraph)
	mux.HandleFunc("POST /api/v1/projects/{id}/wiki/revert", h.revertWiki)
	mux.HandleFunc("POST /api/v1/projects/{id}/wiki/verify", h.verifyWikiPage)
	mux.HandleFunc("POST /api/v1/projects/{id}/wiki/resident", h.setWikiResident)
	mux.HandleFunc("POST /api/v1/projects/{id}/wiki/question", h.wikiQuestion)
	mux.HandleFunc("GET /api/v1/projects/{id}/branches", h.branches)
	mux.HandleFunc("GET /api/v1/projects/{id}/checkout/diff", h.checkoutDiff)
	mux.HandleFunc("POST /api/v1/projects/{id}/checkout/commit", h.commitCheckout)
	mux.HandleFunc("POST /api/v1/projects/{id}/setup", h.startSetup)
	mux.HandleFunc("POST /api/v1/projects/{id}/workspace/pending", h.settlePendingSteps)
	mux.HandleFunc("GET /api/v1/members/{id}/diff", h.memberDiff)
	mux.HandleFunc("POST /api/v1/members/{id}/merge", h.mergeMember)
	mux.HandleFunc("POST /api/v1/members/{id}/sync", h.syncMember)
	mux.HandleFunc("POST /api/v1/members/{id}/merge/abort", h.abortMerge)
	mux.HandleFunc("POST /api/v1/members/{id}/set-aside", h.setAside)
	mux.HandleFunc("GET /api/v1/projects/{id}/wiki/maintainer", h.upkeepStatus)
	mux.HandleFunc("POST /api/v1/projects/{id}/wiki/maintain", h.startUpkeep)
	mux.HandleFunc("GET /api/v1/projects/{id}/memory", h.projectMemory)
	mux.HandleFunc("PUT /api/v1/projects/{id}/memory", h.setProjectMemory)
	mux.HandleFunc("GET /api/v1/memory", h.personalMemory)
	mux.HandleFunc("PUT /api/v1/memory", h.setPersonalMemory)
	mux.HandleFunc("GET /api/v1/memory/history", h.memoryHistory)
	mux.HandleFunc("POST /api/v1/memory/revert", h.revertMemory)

	mux.HandleFunc("GET /api/v1/library", h.libraryCatalog)
	mux.HandleFunc("GET /api/v1/library/page", h.libraryPage)
	mux.HandleFunc("GET /api/v1/wikis", h.listWikis)
	mux.HandleFunc("GET /api/v1/wikis/search", h.searchWikis)
	mux.HandleFunc("GET /api/v1/library/search", h.searchLibrary)
	mux.HandleFunc("GET /api/v1/library/history", h.libraryHistory)
	mux.HandleFunc("GET /api/v1/library/graph", h.libraryGraph)
	mux.HandleFunc("POST /api/v1/library/revert", h.revertLibrary)
	mux.HandleFunc("POST /api/v1/library/verify", h.verifyLibraryPage)
	mux.HandleFunc("POST /api/v1/library/transfer", h.transferSkill)
	mux.HandleFunc("POST /api/v1/library/import", h.importSkill)
	mux.HandleFunc("POST /api/v1/library/install", h.installSkill)
	mux.HandleFunc("GET /api/v1/skills/builtin", h.builtinSkills)
	mux.HandleFunc("POST /api/v1/library/rollback", h.rollbackSkill)
	mux.HandleFunc("GET /api/v1/library/usage", h.skillUses)
	return withCORS(deps.Events.AllowedOrigins, h.requireSession(mux))
}

func (h *handlers) listRuntimes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, RuntimesResponse{Runtimes: h.deps.Runtimes.Run(r.Context())})
}

func (h *handlers) runtimeTraits(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, RuntimeTraitsResponse{Traits: runtime.AllTraits()})
}

func (h *handlers) listMachines(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, MachinesResponse{Machines: h.deps.Machines.Machines()})
}

// probeMachine asks a connected machine to look for its runtimes again, after
// someone installed or signed in to one. The answer comes later: the
// machine's probed_at moves once its report is in.
func (h *handlers) probeMachine(w http.ResponseWriter, r *http.Request) {
	err := h.deps.Machines.Probe(r.Context(), r.PathValue("id"))
	switch {
	case err == nil:
		w.WriteHeader(http.StatusAccepted)
	case errors.Is(err, hub.ErrUnknownMachine):
		writeCoded(w, http.StatusNotFound, "machineOffline", nil, "machine is not connected")
	default:
		h.deps.Logger.Error("probe machine", "machine", r.PathValue("id"), "err", err)
		writeCoded(w, http.StatusBadGateway, "machineUnreachable", nil, "the machine could not be reached")
	}
}

// writeJSON encodes v as the response body with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Encoding a plain struct cannot fail; an error here means the client
	// went away, and there is nothing useful left to do.
	_ = json.NewEncoder(w).Encode(v)
}
