package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// Setting a project up for its members' worktrees (docs/design.md 5.21).
// A worktree holds only what git has. The project's leader looks the
// checkout over once and writes down what a new worktree needs besides:
// what to copy into it from the checkout, and one command to run in it. It
// does so in a turn of its own, a setup, in the project's setup topic, when
// a member first needs a worktree or a person asks. The steps take effect
// at once when the leader runs commands without asking; otherwise a person
// adopts the command first, from a card in the chat. The members waiting
// for a worktree wait for the setup.

// setupRun is one setup of a project by its leader.
type setupRun struct {
	project store.Project
	reason  setupReason
}

// setupReason is why a setup runs.
type setupReason string

const (
	setupForWorktree setupReason = "a member needs a worktree"
	setupAsked       setupReason = "asked by a person"
)

// setupWait is what the turns waiting for a project's setup wait on: done
// closes once it is settled, err saying why it failed.
type setupWait struct {
	done chan struct{}
	err  error
}

// setupPoll is how often a turn waiting for the setup looks at the project
// again: a person may write the steps down meanwhile, or adopt them.
const setupPoll = 2 * time.Second

// setupTopicIntro heads the project's setup topic.
const setupTopicIntro = "Setup: the project's leader sets the project up here, for the members' worktrees."

// setUp reports whether a project is set up for worktrees: it was, and no
// steps wait for a person to adopt.
func setUp(p store.Project) bool {
	return p.InitializedAt != nil && p.WorkspacePending == nil
}

// awaitSetup waits until the project is set up for worktrees, starting its
// leader's setup when none is under way.
func (m *TurnManager) awaitSetup(ctx context.Context, at *activeTurn, project store.Project) error {
	if setUp(project) {
		return nil
	}
	wait := m.setupFor(ctx, project, setupForWorktree)
	if wait == nil {
		return nil
	}
	m.notice(at, runtime.NoticeInfo, "Waiting for the project's leader to set the project up for worktrees.")
	done, told := wait.done, false
	tick := time.NewTicker(setupPoll)
	defer tick.Stop()
	for {
		select {
		case <-done:
			if wait.err != nil {
				return wait.err
			}
			// Settled well; what is left is a person adopting the steps.
			done = nil
		case <-tick.C:
		case <-ctx.Done():
			return ctx.Err()
		}
		p, err := m.store.GetProject(ctx, project.ID)
		if err != nil {
			return err
		}
		if setUp(p) {
			return nil
		}
		if p.WorkspacePending != nil && !told {
			m.notice(at, runtime.NoticeInfo, "Waiting for a person to adopt the setup steps the leader wrote down.")
			told = true
		}
	}
}

// setupFor is the wait for the project's setup: the one under way, or a
// new one, the leader's setup turn queued for it. For a member needing a
// worktree it is nil when the project is set up after all: project was read
// before a setup that has settled since, as when two members woken at once
// both need one.
func (m *TurnManager) setupFor(ctx context.Context, project store.Project, reason setupReason) *setupWait {
	m.mu.Lock()
	if w := m.setups[project.ID]; w != nil {
		m.mu.Unlock()
		return w
	}
	w := &setupWait{done: make(chan struct{})}
	m.setups[project.ID] = w
	m.mu.Unlock()
	if reason == setupForWorktree {
		// A setup settles once the project says so: none is under way
		// now, so the project says so if one has.
		fresh, err := m.store.GetProject(ctx, project.ID)
		if err != nil {
			m.settleSetup(project.ID, err)
			return w
		}
		if setUp(fresh) {
			m.settleSetup(project.ID, nil)
			return nil
		}
		project = fresh
	}
	if err := m.requestSetup(ctx, project, reason); err != nil {
		m.settleSetup(project.ID, err)
	}
	return w
}

// settleSetup ends the wait for a project's setup; err says why it failed.
func (m *TurnManager) settleSetup(projectID string, err error) {
	m.mu.Lock()
	w := m.setups[projectID]
	delete(m.setups, projectID)
	m.mu.Unlock()
	if w != nil {
		w.err = err
		close(w.done)
	}
}

// requestSetup queues the leader's setup turn, announced in the project's
// setup topic.
func (m *TurnManager) requestSetup(ctx context.Context, project store.Project, reason setupReason) error {
	if project.LeaderID == "" {
		return store.Conflicting("noLeader", nil, "the project has no member to lead it and set it up: add one to its chat first")
	}
	leader, err := m.store.GetMember(ctx, project.LeaderID)
	if err != nil {
		return err
	}
	if !leader.Enabled || leader.Removed() {
		return store.Conflicting("leaderOff", store.Params{"name": leader.DisplayName},
			"the project's leader %s is switched off: a person makes another member the leader in the chat's info", leader.DisplayName)
	}
	topic, err := m.setupTopic(ctx, project)
	if err != nil {
		return err
	}
	note, err := m.post(ctx, store.NewMessage{RoomID: topic.RoomID, ThreadID: topic.ID, SenderKind: store.SenderSystem, Body: setupNote(leader.DisplayName, reason)})
	if err != nil {
		return err
	}
	return m.TriggerSetup(ctx, leader, note, topic, &setupRun{project: project, reason: reason})
}

// setupNote is what the setup topic says as a setup starts, in words the
// UI knows to put its own way.
func setupNote(leader string, reason setupReason) string {
	return fmt.Sprintf("Project setup by %s (%s).", leader, reason)
}

// TriggerSetup queues the leader's setup turn, announced by note in the
// setup topic. It runs on its own, never merged with what the member is
// asked in the chat.
func (m *TurnManager) TriggerSetup(ctx context.Context, member store.Member, note store.Message, topic store.Thread, run *setupRun) error {
	m.mu.Lock()
	st := m.state(member.ID)
	if st.starting || st.running != nil {
		st.pending = append(st.pending, trigger{msg: note, thread: topic, setup: run})
		m.mu.Unlock()
		return nil
	}
	st.starting = true
	m.mu.Unlock()
	m.launch(member.ID, topic, []store.Message{note}, nil, run)
	return nil
}

// setupTopic is the project's setup topic, opened the first time it is
// needed.
func (m *TurnManager) setupTopic(ctx context.Context, project store.Project) (store.Thread, error) {
	fresh, err := m.store.GetProject(ctx, project.ID)
	if err != nil {
		return store.Thread{}, err
	}
	if fresh.SetupThreadID != "" {
		return m.store.GetThread(ctx, fresh.SetupThreadID)
	}
	root, err := m.store.CreateMessage(ctx, store.NewMessage{RoomID: fresh.MainRoomID, SenderKind: store.SenderSystem, Body: setupTopicIntro})
	if err != nil {
		return store.Thread{}, err
	}
	thread, err := m.store.ThreadForMessage(ctx, root.ID)
	if err != nil {
		return store.Thread{}, err
	}
	set, err := m.store.SetProjectSetupThread(ctx, fresh.ID, thread.ID)
	if err != nil {
		return store.Thread{}, err
	}
	if !set {
		// Another opened it first: that one is the topic.
		again, err := m.store.GetProject(ctx, fresh.ID)
		if err != nil {
			return store.Thread{}, err
		}
		return m.store.GetThread(ctx, again.SetupThreadID)
	}
	// Announced once the project names it, as the wiki topic is.
	m.publish(Event{Kind: EventMessage, RoomID: root.Room, At: root.CreatedAt, Message: &root, Thread: topicSummary(thread)})
	return thread, nil
}

// setupBrief is what the leader is told in its setup turn.
func setupBrief(run *setupRun, leader store.Member, project store.Project) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are %q, the leader of the project %q. This is not a chat turn: you are setting the project up so the other members can work in git worktrees of their own. "+
		"Nobody waits for a reply and nobody will answer questions or approve plans, so do the work yourself.\n", leader.DisplayName, project.Name)
	if about := strings.TrimSpace(project.Description); about != "" {
		b.WriteString("\nAbout the project:\n" + about + "\n")
	}
	fmt.Fprintf(&b, "\nThe project's checkout is %s, where you work. Each other member gets a git worktree of it, made from the commit the checkout is at: "+
		"it holds what is committed to git and nothing else. No installed dependencies, no local settings such as .env, none of the files git ignores or does not track, "+
		"none of the changes not committed yet.\n", project.RepoPath)
	if len(project.WorkspaceCopy) > 0 || project.WorkspaceRun != "" {
		fmt.Fprintf(&b, "\nWhat a new worktree gets now: %s. Keep what still holds, set right what does not.\n", stepsLine(store.WorkspaceSteps{Copy: project.WorkspaceCopy, Run: project.WorkspaceRun}))
	}
	b.WriteString("\nWhat to do:\n" +
		"1. Look the checkout over: what it needs to build, run and test (the README, package manifests and lock files, .gitignore), and what is here but not in git (git status --ignored lists it).\n" +
		"2. If the checkout itself cannot build or run yet, set it up the way the project expects, such as installing its dependencies. Do not change files git tracks.\n" +
		"3. Call " + runtime.SetupToolSteps + " with what a new worktree needs besides what git has: copy, the paths relative to the checkout to copy into it " +
		"(local settings, documents kept out of git that the work relies on), and run, one shell command run in it afterwards (installing dependencies, say; $VEYLOOM_REPO is the checkout's path). " +
		"Leave out what the members can do without, and what is large and quick to make again; call it with neither when a worktree needs nothing more.\n" +
		"4. Say in a line or two what you set up.\n")
	return b.String()
}

// setupDone settles the setup a turn of the leader ran: the project is set
// up unless the turn failed, or its steps wait for a person to adopt.
func (m *TurnManager) setupDone(ctx context.Context, at *activeTurn, status store.TurnStatus, reason string) {
	projectID := at.setup.project.ID
	if status != store.TurnDone {
		m.settleSetup(projectID, fmt.Errorf("the leader's setup did not finish: %s", cmpOr(reason, string(status))))
		return
	}
	p, err := m.store.GetProject(ctx, projectID)
	if err != nil {
		m.settleSetup(projectID, err)
		return
	}
	if p.WorkspacePending != nil {
		// A person adopts them, or turns them down.
		return
	}
	if err := m.store.MarkProjectInitialized(ctx, projectID); err != nil {
		m.settleSetup(projectID, err)
		return
	}
	m.settleSetup(projectID, nil)
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// answerSetupSteps writes down the steps the leader gave with its tool:
// at once when it runs commands without asking or gave none, otherwise for
// a person to adopt, shown to them on a card in the turn's topic.
func (m *TurnManager) answerSetupSteps(ctx context.Context, at *activeTurn, q runtime.RoomQuery) (string, error) {
	project, err := m.store.RoomProject(ctx, at.member.RoomID)
	if err != nil {
		return "", err
	}
	if at.member.ID != project.LeaderID {
		return "", errors.New("only the project's leader writes down how the members' worktrees are got ready")
	}
	var args struct {
		Copy []string `json:"copy"`
		Run  string   `json:"run"`
	}
	if len(q.Args) > 0 {
		if err := json.Unmarshal(q.Args, &args); err != nil {
			return "", fmt.Errorf("the arguments: %w", err)
		}
	}
	steps, err := store.CleanWorkspaceSteps(store.WorkspaceSteps{Copy: args.Copy, Run: args.Run})
	if err != nil {
		return "", err
	}
	preset := cmpOr(string(at.member.PermissionPreset), string(at.agent.PermissionPreset))
	if steps.Run == "" || preset == string(store.PermissionFullAuto) {
		if err := m.store.SetWorkspaceSteps(ctx, project.ID, steps); err != nil {
			return "", err
		}
		m.supersedeDrafts(ctx, project.ID, setupSubject)
		m.settleSetup(project.ID, nil)
		return "Written down. Every new worktree now gets: " + stepsLine(steps) + ".", nil
	}
	// A card a person adopts, drafted as any other (design.md 5.23.5); the
	// project keeps the steps as waiting too, which is what new worktrees
	// wait on.
	d, err := m.draft(ctx, at, store.NewDraft{
		RoomID: at.thread.RoomID, ThreadID: at.thread.ID, MemberID: at.member.ID, TurnID: at.turn.ID,
		Kind: store.DraftSetupSteps, Subject: setupSubject, Params: store.DraftParams{Steps: &steps},
	}, store.Member{})
	if err != nil {
		return "", err
	}
	if err := m.store.SetWorkspacePending(ctx, project.ID, steps, d.MessageID); err != nil {
		m.untellable(d.ID)
		return "", err
	}
	return "Written down. A person adopts the command before it takes effect; until then new worktrees wait. The steps: " + stepsLine(steps) + ".", nil
}

// setupSubject is what the drafts of setup steps are about: a project has
// one set waiting for a person at a time.
const setupSubject = "setup"

// supersedeDrafts has the project's pending drafts about subject give way:
// what they were about was settled otherwise.
func (m *TurnManager) supersedeDrafts(ctx context.Context, projectID, subject string) {
	gone, err := m.store.SupersedeDrafts(ctx, projectID, subject)
	if err != nil {
		m.logger.Error("supersede drafts", "project", projectID, "err", err)
		return
	}
	for _, d := range gone {
		m.publish(draftEvent(d))
	}
}

// tellLeader tells the project's leader, in the setup topic, that getting
// a member's worktree ready failed, and wakes it to set the steps right.
func (m *TurnManager) tellLeader(project store.Project, member store.Member, res protocol.WorkspaceResult) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
		defer cancel()
		if project.LeaderID == "" {
			return
		}
		leader, err := m.store.GetMember(ctx, project.LeaderID)
		if err != nil || !leader.Enabled || leader.Removed() {
			return
		}
		topic, err := m.setupTopic(ctx, project)
		if err != nil {
			m.logger.Error("tell the leader of a failed worktree", "project", project.ID, "err", err)
			return
		}
		body := fmt.Sprintf("@%s getting %s's worktree ready failed: %s.", leader.DisplayName, member.DisplayName, res.Error)
		if out := strings.TrimSpace(res.Text); out != "" {
			body += "\n\n```\n" + out + "\n```"
		}
		body += "\n\nSet the steps right with " + runtime.SetupToolSteps + "; " + member.DisplayName + " is asked again once they are."
		note, err := m.post(ctx, store.NewMessage{
			RoomID: topic.RoomID, ThreadID: topic.ID, SenderKind: store.SenderSystem, Body: body,
			Mentions: []store.Mention{{Kind: store.MentionAgent, ID: leader.ID}},
		})
		if err != nil {
			m.logger.Error("tell the leader of a failed worktree", "project", project.ID, "err", err)
			return
		}
		if err := m.TriggerIn(ctx, leader, note, topic); err != nil {
			m.logger.Error("wake the leader", "project", project.ID, "err", err)
		}
	}()
}

// errStepsDropped is what the turns waiting for a project's setup hear when
// a person turns the leader's steps down.
var errStepsDropped = store.Conflicting("stepsDropped", nil,
	"a person turned the leader's setup steps down: write the steps down in the project's settings, or ask the leader again")

// StartSetup has the project's leader set the project up, as a person
// asked (docs/design.md 5.21): what a new worktree needs is looked over
// and written down anew. A setup under way already is the one asked for.
func (h *Hub) StartSetup(ctx context.Context, projectID string) error {
	project, err := h.store.GetProject(ctx, projectID)
	if err != nil {
		return err
	}
	wait := h.turns.setupFor(ctx, project, setupAsked)
	select {
	case <-wait.done:
		// Settled at once: it could not start.
		return wait.err
	default:
		return nil
	}
}

// SettleWorkspaceSteps adopts the setup steps waiting for a person, or
// turns them down, as the person userID (docs/design.md 5.21): their card
// (5.23.5) says so. The members' turns waiting for them go on, or end
// saying the steps were turned down.
func (h *Hub) SettleWorkspaceSteps(ctx context.Context, projectID, userID string, adopt bool) error {
	if d, err := h.store.OpenDraft(ctx, projectID, setupSubject); err == nil {
		if adopt {
			_, err = h.RunDraft(ctx, d.ID, userID, DraftEdit{})
		} else {
			_, err = h.DeclineDraft(ctx, d.ID, userID)
		}
		return err
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	// Steps waiting with no card: written down before steps were drafts.
	if adopt {
		return h.adoptSteps(ctx, projectID)
	}
	return h.dropSteps(ctx, projectID)
}

// adoptSteps makes the steps waiting for a person the project's; the
// members waiting for them go on.
func (h *Hub) adoptSteps(ctx context.Context, projectID string) error {
	if err := h.store.AdoptWorkspacePending(ctx, projectID); err != nil {
		return err
	}
	h.turns.settleSetup(projectID, nil)
	return nil
}

// dropSteps turns the steps waiting for a person down; the members waiting
// for them are told.
func (h *Hub) dropSteps(ctx context.Context, projectID string) error {
	if err := h.store.DropWorkspacePending(ctx, projectID); err != nil {
		return err
	}
	h.turns.settleSetup(projectID, errStepsDropped)
	return nil
}

// WorkspaceStepsWritten tells the turns waiting for the project's setup
// that a person wrote the steps down: the project is set up, and steps a
// card offered give way to theirs.
func (h *Hub) WorkspaceStepsWritten(projectID string) {
	ctx, cancel := context.WithTimeout(context.Background(), h.cfg.StoreTimeout)
	defer cancel()
	h.turns.supersedeDrafts(ctx, projectID, setupSubject)
	h.turns.settleSetup(projectID, nil)
}
