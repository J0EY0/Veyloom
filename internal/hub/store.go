package hub

import (
	"context"

	"github.com/J0EY0/veyloom/internal/store"
)

// Store is everything the hub needs from persistence. *store.Store
// satisfies it. Tests substitute fakes for the slices they exercise; the
// smaller interfaces below name those slices so a fake need not implement
// the whole thing.
type Store interface {
	WorkerStore
	messageStore
	agentStore
	turnStore
	approvalStore
}

// messageStore is the message and thread access the hub uses.
type messageStore interface {
	CreateMessage(ctx context.Context, m store.NewMessage) (store.Message, error)
	GetMessage(ctx context.Context, id string) (store.Message, error)
	ListThreadMessagesBefore(ctx context.Context, threadID string, before int64, limit int) ([]store.Message, error)
	ThreadForMessage(ctx context.Context, messageID string) (store.Thread, error)
	LastAgentMessageInThread(ctx context.Context, threadID string) (store.Message, error)
	GetUser(ctx context.Context, id string) (store.User, error)
}

// agentStore is the agent access the hub uses.
type agentStore interface {
	GetAgentInstance(ctx context.Context, id string) (store.AgentInstance, error)
	ListRoomAgentInstances(ctx context.Context, roomID string) ([]store.AgentInstance, error)
	GetAgentTemplate(ctx context.Context, id string) (store.AgentTemplate, error)
	UpdateAgentInstanceSession(ctx context.Context, instanceID, sessionRef string) error
}

// turnStore is the turn access the hub uses.
type turnStore interface {
	CreateTurn(ctx context.Context, t store.NewTurn) (store.Turn, error)
	FinishTurn(ctx context.Context, id string, out store.TurnOutcome) (store.Turn, error)
}

// approvalStore is the approval access the hub uses.
type approvalStore interface {
	CreateApproval(ctx context.Context, a store.NewApproval) (store.Approval, error)
	DecideApproval(ctx context.Context, id string, out store.ApprovalOutcome) (store.Approval, error)
	ResolveTurnApprovals(ctx context.Context, turnID string, status store.ApprovalStatus, message string) ([]store.Approval, error)
}
