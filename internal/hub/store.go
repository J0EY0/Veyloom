package hub

import (
	"context"
	"time"

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
	lookupStore
	reviewStore
	graphStore
	worktreeStore
	relayStore
	reminderStore
	draftStore
	attachmentStore
}

// draftStore is what drafts for a person to run need of the store
// (docs/design.md 5.23.5).
type draftStore interface {
	CreateDraft(ctx context.Context, d store.NewDraft) (store.Draft, []store.Draft, error)
	SetDraftMessage(ctx context.Context, id, messageID string) error
	SetDraftResultMessage(ctx context.Context, id, messageID string) error
	GetDraft(ctx context.Context, id string) (store.Draft, error)
	GetDraftByMessage(ctx context.Context, messageID string) (store.Draft, error)
	OpenDraft(ctx context.Context, projectID, subject string) (store.Draft, error)
	SupersedeDrafts(ctx context.Context, projectID, subject string) ([]store.Draft, error)
	ListThreadDrafts(ctx context.Context, threadID string) ([]store.Draft, error)
	ClaimDraft(ctx context.Context, id string) (store.Draft, error)
	ReleaseDraft(ctx context.Context, id string) (store.Draft, error)
	SettleDraft(ctx context.Context, id string, status store.DraftStatus, result store.DraftResult, userID string) (store.Draft, error)
	DeclineDraft(ctx context.Context, id, userID string) (store.Draft, error)
}

// reminderStore is what members' reminders to themselves need of the
// store (docs/design.md 5.23.4).
type reminderStore interface {
	CreateReminder(ctx context.Context, r store.NewReminder) (store.Reminder, error)
	SetReminderMessage(ctx context.Context, id, messageID string) error
	GetReminder(ctx context.Context, id string) (store.Reminder, error)
	CountPendingReminders(ctx context.Context, memberID string) (int, error)
	ListPendingReminders(ctx context.Context, machineID string) ([]store.Reminder, error)
	ListMemberPendingReminders(ctx context.Context, memberID string) ([]store.Reminder, error)
	ListThreadReminders(ctx context.Context, threadID string) ([]store.Reminder, error)
	FireReminder(ctx context.Context, id string) (store.Reminder, error)
	SetReminderFired(ctx context.Context, id, messageID string) error
	CancelReminder(ctx context.Context, id, userID string) (store.Reminder, error)
	DropReminder(ctx context.Context, id string) (store.Reminder, error)
}

// attachmentStore is what sweeping away uploads nobody sent needs of the
// store (docs/webui.md 4.21).
type attachmentStore interface {
	UnclaimedAttachments(ctx context.Context, before time.Time, limit int) ([]store.Attachment, error)
	DeleteUnclaimedAttachment(ctx context.Context, id string) (store.Attachment, error)
}

// relayStore is what agents waking one another needs of the store
// (docs/design.md 5.22).
type relayStore interface {
	ChainWakes(ctx context.Context, chainMessageID string, recent int) (woken int, worked []bool, err error)
	CreateRelayHold(ctx context.Context, h store.RelayHold) error
	GetRelayHold(ctx context.Context, messageID string) (store.RelayHold, error)
	ContinueRelayHold(ctx context.Context, messageID string) (store.RelayHold, error)
	// What busy members were asked, kept until their turns start.
	QueueWake(ctx context.Context, w store.QueuedWake) error
	UnqueueWakes(ctx context.Context, memberID string, messageIDs []string) error
	ListQueuedWakes(ctx context.Context, machineID string) ([]store.QueuedWake, error)
	// What keeps turns from starting that would only fail (pauses.go).
	PauseAccount(ctx context.Context, machineID, runtime string, reason store.PauseReason, detail string, endsAt *time.Time) (store.Pause, error)
	PauseMember(ctx context.Context, memberID string, reason store.PauseReason, detail string, endsAt *time.Time) (store.Pause, error)
	ListPauses(ctx context.Context) ([]store.Pause, error)
	LiftAccountPause(ctx context.Context, machineID, runtime string) (bool, error)
	LiftMemberPause(ctx context.Context, memberID string) (bool, error)
}

// messageStore is the message and thread access the hub uses.
type messageStore interface {
	CreateMessage(ctx context.Context, m store.NewMessage) (store.Message, error)
	// ChainWork is a piece of work across its topics.
	ChainWork(ctx context.Context, chain string) (store.WorkSummary, error)
	GetMessage(ctx context.Context, id string) (store.Message, error)
	ListThreadMessagesBefore(ctx context.Context, threadID string, before int64, limit int) ([]store.Message, error)
	ThreadForMessage(ctx context.Context, messageID string) (store.Thread, error)
	ThreadOfMessage(ctx context.Context, messageID string) (store.Thread, error)
	GetThread(ctx context.Context, id string) (store.Thread, error)
	UpdateMessageBody(ctx context.Context, id, body, turnID string, mentions []store.Mention) (store.Message, error)
	SetMessageTitle(ctx context.Context, id, title string) (store.Message, error)
	LastAgentMessageInThread(ctx context.Context, threadID string) (store.Message, error)
	GetUser(ctx context.Context, id string) (store.User, error)
	// What a person read of their inbox.
	MarkMentionsRead(ctx context.Context, userID string, read store.InboxRead) (int, error)
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
	// A person letting the rest of a turn's requests through, and taking
	// it back (docs/design.md 4.6).
	TrustTurn(ctx context.Context, turnID, userID string) (store.Turn, error)
	UntrustTurn(ctx context.Context, turnID string) (store.Turn, error)
}

// approvalStore is the approval access the hub uses.
type approvalStore interface {
	CreateApproval(ctx context.Context, a store.NewApproval) (store.Approval, error)
	CreateReviewedApproval(ctx context.Context, a store.NewReviewedApproval) (store.Approval, error)
	GetApproval(ctx context.Context, id string) (store.Approval, error)
	DecideApproval(ctx context.Context, id string, out store.ApprovalOutcome) (store.Approval, error)
	ResolveTurnApprovals(ctx context.Context, turnID string, status store.ApprovalStatus, message string) ([]store.Approval, error)
	// What people allowed members always (docs/design.md 4.6).
	ListMemberRules(ctx context.Context, memberID, runtime string) ([]store.MemberRule, error)
	AddMemberRules(ctx context.Context, in store.NewMemberRules) ([]store.MemberRule, error)
}
