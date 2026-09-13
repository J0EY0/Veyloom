// Package api exposes the hub over HTTP.
//
// Everything is JSON and versioned under /api/v1 so later additions do not
// break clients. Handlers depend on small interfaces so tests can substitute
// in-memory fakes; the real implementations are *worker.Discovery,
// *hub.Hub and *store.Store.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/J0EY0/veyloom/internal/engine"
	"github.com/J0EY0/veyloom/internal/hub"
	"github.com/J0EY0/veyloom/internal/store"
)

// Discoverer runs engine discovery on this machine.
type Discoverer interface {
	Run(ctx context.Context) []engine.Info
}

// WorkerLister reports the workers connected to the hub.
type WorkerLister interface {
	Workers() []hub.WorkerInfo
}

// ProjectStore persists projects and their rooms.
type ProjectStore interface {
	CreateProject(ctx context.Context, p store.NewProject) (store.Project, store.Room, error)
	GetProject(ctx context.Context, id string) (store.Project, error)
	ListProjects(ctx context.Context) ([]store.Project, error)
	CreateRoom(ctx context.Context, projectID, name string) (store.Room, error)
	GetRoom(ctx context.Context, id string) (store.Room, error)
	ListRooms(ctx context.Context, projectID string) ([]store.Room, error)
}

// Deps are the collaborators the handlers need.
type Deps struct {
	Engines   Discoverer
	Workers   WorkerLister
	Projects  ProjectStore
	Users     UserStore
	Messages  MessageStore
	Agents    AgentStore
	Turns     TurnStore
	Approvals ApprovalStore
	// Chat posts user messages, controls turns, settles approvals and
	// streams room events; it is the hub.
	Chat Chat
	// Events tunes the WebSocket event stream.
	Events EventsOptions
	// Logger receives unexpected errors. nil means slog.Default().
	Logger *slog.Logger
}

// EnginesResponse is the body of GET /api/v1/engines: the engines found on
// the machine serving the request.
type EnginesResponse struct {
	Engines []engine.Info `json:"engines"`
}

// WorkersResponse is the body of GET /api/v1/workers: every worker connected
// to the hub, with the engines each one offers.
type WorkersResponse struct {
	Workers []hub.WorkerInfo `json:"workers"`
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
	mux.HandleFunc("GET /api/v1/engines", h.listEngines)
	mux.HandleFunc("GET /api/v1/workers", h.listWorkers)

	mux.HandleFunc("POST /api/v1/projects", h.createProject)
	mux.HandleFunc("GET /api/v1/projects", h.listProjects)
	mux.HandleFunc("GET /api/v1/projects/{id}", h.getProject)
	mux.HandleFunc("POST /api/v1/projects/{id}/rooms", h.createRoom)
	mux.HandleFunc("GET /api/v1/projects/{id}/rooms", h.listRooms)
	mux.HandleFunc("GET /api/v1/rooms/{id}", h.getRoom)

	mux.HandleFunc("POST /api/v1/users", h.createUser)
	mux.HandleFunc("GET /api/v1/users", h.listUsers)
	mux.HandleFunc("GET /api/v1/users/{id}", h.getUser)

	mux.HandleFunc("POST /api/v1/rooms/{id}/messages", h.postMessage)
	mux.HandleFunc("GET /api/v1/rooms/{id}/messages", h.listRoomMessages)
	mux.HandleFunc("GET /api/v1/messages/{id}", h.getMessage)
	mux.HandleFunc("GET /api/v1/threads/{id}", h.getThread)
	mux.HandleFunc("GET /api/v1/threads/{id}/messages", h.listThreadMessages)

	mux.HandleFunc("POST /api/v1/agent-templates", h.createAgentTemplate)
	mux.HandleFunc("GET /api/v1/agent-templates", h.listAgentTemplates)
	mux.HandleFunc("GET /api/v1/agent-templates/{id}", h.getAgentTemplate)
	mux.HandleFunc("PUT /api/v1/agent-templates/{id}", h.updateAgentTemplate)
	mux.HandleFunc("POST /api/v1/rooms/{id}/agents", h.createAgent)
	mux.HandleFunc("GET /api/v1/rooms/{id}/agents", h.listRoomAgents)
	mux.HandleFunc("GET /api/v1/agents/{id}", h.getAgent)

	mux.HandleFunc("GET /api/v1/turns/{id}", h.getTurn)
	mux.HandleFunc("POST /api/v1/turns/{id}/cancel", h.cancelTurn)
	mux.HandleFunc("GET /api/v1/rooms/{id}/turns", h.listRoomTurns)

	mux.HandleFunc("GET /api/v1/approvals/{id}", h.getApproval)
	mux.HandleFunc("POST /api/v1/approvals/{id}/decide", h.decideApproval)
	mux.HandleFunc("GET /api/v1/rooms/{id}/approvals", h.listRoomApprovals)
	mux.HandleFunc("GET /api/v1/turns/{id}/approvals", h.listTurnApprovals)

	mux.HandleFunc("GET /api/v1/rooms/{id}/events", h.roomEvents)
	return mux
}

func (h *handlers) listEngines(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, EnginesResponse{Engines: h.deps.Engines.Run(r.Context())})
}

func (h *handlers) listWorkers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, WorkersResponse{Workers: h.deps.Workers.Workers()})
}

// writeJSON encodes v as the response body with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Encoding a plain struct cannot fail; an error here means the client
	// went away, and there is nothing useful left to do.
	_ = json.NewEncoder(w).Encode(v)
}
