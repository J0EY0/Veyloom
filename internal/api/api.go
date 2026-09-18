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

	"github.com/J0EY0/veyloom/internal/hub"
	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
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
	// Logger receives unexpected errors. nil means slog.Default().
	Logger *slog.Logger
}

// RuntimesResponse is the body of GET /api/v1/runtimes: the runtimes found on
// the machine serving the request.
type RuntimesResponse struct {
	Runtimes []runtime.Info `json:"runtimes"`
}

// MachinesResponse is the body of GET /api/v1/machines: every machine connected
// to the hub, with the runtimes each one offers.
type MachinesResponse struct {
	Machines []hub.MachineInfo `json:"machines"`
}

// handlers groups the HTTP handlers around their dependencies.
type handlers struct {
	deps Deps
}

// NewHandler builds the HTTP handler for the hub API.
func NewHandler(deps Deps) http.Handler {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	h := &handlers{deps: deps}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/runtimes", h.listRuntimes)
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
	mux.HandleFunc("POST /api/v1/me/password", h.changePassword)
	mux.HandleFunc("POST /api/v1/users", h.createUser)
	mux.HandleFunc("GET /api/v1/users", h.listUsers)
	mux.HandleFunc("GET /api/v1/users/{id}", h.getUser)
	mux.HandleFunc("GET /api/v1/users/{id}/inbox", h.userInbox)

	mux.HandleFunc("POST /api/v1/rooms/{id}/messages", h.postMessage)
	mux.HandleFunc("GET /api/v1/rooms/{id}/messages", h.listRoomMessages)
	mux.HandleFunc("GET /api/v1/messages/{id}", h.getMessage)
	mux.HandleFunc("GET /api/v1/threads/{id}", h.getThread)
	mux.HandleFunc("GET /api/v1/threads/{id}/messages", h.listThreadMessages)
	mux.HandleFunc("POST /api/v1/rooms/{id}/attachments", h.uploadAttachment)
	mux.HandleFunc("GET /api/v1/attachments/{id}", h.getAttachment)
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

	mux.HandleFunc("GET /api/v1/turns/{id}", h.getTurn)
	mux.HandleFunc("POST /api/v1/turns/{id}/cancel", h.cancelTurn)
	mux.HandleFunc("GET /api/v1/turns/{id}/transcript", h.turnTranscript)
	mux.HandleFunc("GET /api/v1/rooms/{id}/turns", h.listRoomTurns)
	mux.HandleFunc("GET /api/v1/topics", h.listTopics)

	mux.HandleFunc("GET /api/v1/approvals", h.listApprovals)
	mux.HandleFunc("GET /api/v1/approvals/{id}", h.getApproval)
	mux.HandleFunc("POST /api/v1/approvals/{id}/decide", h.decideApproval)
	mux.HandleFunc("GET /api/v1/rooms/{id}/approvals", h.listRoomApprovals)
	mux.HandleFunc("GET /api/v1/turns/{id}/approvals", h.listTurnApprovals)

	mux.HandleFunc("GET /api/v1/rooms/{id}/events", h.roomEvents)
	return withCORS(deps.Events.AllowedOrigins, h.requireSession(mux))
}

func (h *handlers) listRuntimes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, RuntimesResponse{Runtimes: h.deps.Runtimes.Run(r.Context())})
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
		writeError(w, http.StatusNotFound, "machine is not connected")
	default:
		h.deps.Logger.Error("probe machine", "machine", r.PathValue("id"), "err", err)
		writeError(w, http.StatusBadGateway, "the machine could not be reached")
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
