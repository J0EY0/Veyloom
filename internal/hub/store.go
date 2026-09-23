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
	MachineStore
	messageStore
	agentStore
	sessionStore
	turnStore
	approvalStore
	wikiStore
	upkeepStore
	reviewStore
	graphStore
}

// messageStore is the message and thread access the hub uses.
type messageStore interface {
	CreateMessage(ctx context.Context, m store.NewMessage) (store.Message, error)
	GetMessage(ctx context.Context, id string) (store.Message, error)
	ListThreadMessagesBefore(ctx context.Context, threadID string, before int64, limit int) ([]store.Message, error)
	ThreadForMessage(ctx context.Context, messageID string) (store.Thread, error)
	ThreadOfMessage(ctx context.Context, messageID string) (store.Thread, error)
	GetThread(ctx context.Context, id string) (store.Thread, error)
	UpdateMessageBody(ctx context.Context, id, body, turnID string, mentions []store.Mention) (store.Message, error)
	LastAgentMessageInThread(ctx context.Context, threadID string) (store.Message, error)
	GetUser(ctx context.Context, id string) (store.User, error)
	// A file people sent, which a page of the wiki may keep.
	GetAttachment(ctx context.Context, id string) (store.Attachment, error)
	// What the room tools read (see roomStore).
	ThreadByNumber(ctx context.Context, roomID string, number int) (store.Thread, error)
	ListRoomTopics(ctx context.Context, roomID string, before int64, limit int) ([]store.TopicListing, error)
	ListRoomMessagesBefore(ctx context.Context, roomID string, before int64, limit int) ([]store.Message, error)
	ListThreadTurns(ctx context.Context, threadID string) ([]store.Turn, error)
	SearchRoomMessages(ctx context.Context, roomID, phrase string, before int64, limit int) ([]store.RoomNewsItem, error)
	// What a brief is put together from (see briefStore).
	RoomProject(ctx context.Context, roomID string) (store.Project, error)
	RoomPosition(ctx context.Context, roomID string) (int64, error)
	RoomNews(ctx context.Context, q store.NewsQuery) ([]store.RoomNewsItem, int, error)
	TopicNews(ctx context.Context, q store.NewsQuery, exceptThreadID string) ([]store.TopicNewsItem, int, error)
	ThreadNews(ctx context.Context, threadID string, q store.NewsQuery) ([]store.Message, int, error)
}

// agentStore is the agent access the hub uses.
type agentStore interface {
	GetMember(ctx context.Context, id string) (store.Member, error)
	ListRoomMembers(ctx context.Context, roomID string) ([]store.Member, error)
	GetAgent(ctx context.Context, id string) (store.Agent, error)
}

// turnStore is the turn access the hub uses.
type turnStore interface {
	ListRoomTurns(ctx context.Context, roomID string, limit int) ([]store.Turn, error)
	CreateTurn(ctx context.Context, t store.NewTurn) (store.Turn, error)
	FinishTurn(ctx context.Context, id string, out store.TurnOutcome) (store.Turn, error)
	SetTurnSession(ctx context.Context, turnID, sessionID string) error
}

// approvalStore is the approval access the hub uses.
type approvalStore interface {
	CreateApproval(ctx context.Context, a store.NewApproval) (store.Approval, error)
	CreateReviewedApproval(ctx context.Context, a store.NewReviewedApproval) (store.Approval, error)
	GetApproval(ctx context.Context, id string) (store.Approval, error)
	DecideApproval(ctx context.Context, id string, out store.ApprovalOutcome) (store.Approval, error)
	ResolveTurnApprovals(ctx context.Context, turnID string, status store.ApprovalStatus, message string) ([]store.Approval, error)
}
