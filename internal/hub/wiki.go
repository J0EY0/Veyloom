package hub

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// The project wikis (docs/design.md 5.3 to 5.5, 5.9 and 5.15). Each
// project's is an OKF bundle in a folder of the hub's state dir that the
// hub alone writes, with a local git repository for its history. Agents
// reach it through the wiki tools (wikitools.go): what they write lands at
// once and becomes one commit when their turn ends; a person undoes what
// they do not want.

// wikiStore is what the wikis need from the database: the projects they
// belong to, the agents skills are installed for, and skills' trials.
type wikiStore interface {
	GetProject(ctx context.Context, id string) (store.Project, error)
	ListProjects(ctx context.Context) ([]store.Project, error)
	GetProjectBySlug(ctx context.Context, slug string) (store.Project, error)
	SetProjectWikiThread(ctx context.Context, projectID, threadID string) (bool, error)
	ListSkillUses(ctx context.Context, skill string, limit int) ([]store.SkillUse, error)
	ListSkillAgents(ctx context.Context, skill string) ([]store.AgentRef, error)
	SetAgentSkill(ctx context.Context, agentID, skill string, installed bool) error
	GetTurn(ctx context.Context, id string) (store.Turn, error)
	StartSkillTrial(ctx context.Context, in store.NewSkillTrial) (store.SkillTrial, error)
	OpenSkillTrial(ctx context.Context, skill string) (store.SkillTrial, error)
	LatestSkillTrial(ctx context.Context, skill string) (store.SkillTrial, error)
	EndSkillTrial(ctx context.Context, id string, status store.SkillTrialStatus, endedBy, reason string) (store.SkillTrial, error)
	SkillTrialUses(ctx context.Context, skill string, since time.Time) (done, failed int, err error)
	ListOpenSkillTrials(ctx context.Context, skills []string) ([]store.SkillTrial, error)
}

// ErrNoWikis answers wiki calls on a hub configured without a wiki dir.
var ErrNoWikis = errors.New("this Veyloom keeps no wikis")

// wikiShelf opens each project's wiki the first time it is needed and keeps
// it open: a bundle holds its index in memory.
type wikiShelf struct {
	root string
	// person names the account, whose edits made outside Veyloom (in an
	// editor, say) are committed under its name.
	person func() string
	git    bool
	logger *slog.Logger
	// changed tells a room its project's wiki changed; nil tells no one.
	changed func(projectID, roomID string)
	// edited hears of the pages a person changed outside Veyloom, by the
	// project whose wiki they are in, "" for the skill library; nil for
	// no one.
	edited func(ctx context.Context, projectID string, pages []string)
	// budgets cap the memories; now dates their entries.
	budgets memoryBudgets
	now     func() time.Time
	// prefs are the account's memory switches, read as each turn asks.
	prefs func() store.MemoryPrefs

	mu       sync.Mutex
	bundles  map[string]*wiki.Bundle // by project ID
	lib      *wiki.Bundle            // the skill library, once opened
	personal *wiki.Bundle            // the personal memory, once opened
	foreign  map[string]*wiki.Bundle // bundles mounted read-only, by folder
}

func newWikiShelf(root string, person func() string, logger *slog.Logger) *wikiShelf {
	_, err := exec.LookPath("git")
	if root != "" && err != nil {
		logger.Warn("git was not found: the wikis keep no history", "err", err)
	}
	return &wikiShelf{
		root: root, person: person, git: err == nil, logger: logger, now: time.Now,
		prefs:   func() store.MemoryPrefs { return store.DefaultMemoryPrefs },
		budgets: memoryBudgets{Personal: DefaultConfig().MemoryPersonalChars, Project: DefaultConfig().MemoryProjectChars},
		bundles: make(map[string]*wiki.Bundle), foreign: make(map[string]*wiki.Bundle),
	}
}

// project opens a project's wiki.
func (s *wikiShelf) project(ctx context.Context, p store.Project) (*wiki.Bundle, error) {
	if s == nil || s.root == "" {
		return nil, ErrNoWikis
	}
	if p.WikiSlug == "" {
		return nil, fmt.Errorf("project %s has no wiki folder", p.ID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if b := s.bundles[p.ID]; b != nil {
		return b, nil
	}
	b, err := wiki.Open(ctx, filepath.Join(s.root, "projects", p.WikiSlug), wiki.Options{
		Layout: wiki.ProjectLayout,
		Git:    s.git,
		Human:  s.human(),
	})
	if err != nil {
		return nil, err
	}
	s.bundles[p.ID] = b
	return b, nil
}

// sync picks up what was edited outside Veyloom since the wiki was last
// looked at, and tells the room when there was something.
func (s *wikiShelf) sync(ctx context.Context, b *wiki.Bundle, projectID, roomID string) {
	changed, err := b.Sync(ctx)
	if err != nil {
		s.logger.Warn("pick up wiki edits", "project", projectID, "err", err)
	}
	if len(changed) > 0 {
		s.notify(projectID, roomID)
		if s.edited != nil {
			s.edited(ctx, projectID, changed)
		}
	}
}

// notify tells a room its project's wiki changed. A projectID of ""
// stands for the skill library.
func (s *wikiShelf) notify(projectID, roomID string) {
	if s.changed != nil && roomID != "" {
		s.changed(projectID, roomID)
	}
}

// library opens the skill library, the one bundle every project shares
// (design.md 5.10), in library/ beside the projects' wikis.
func (s *wikiShelf) library(ctx context.Context) (*wiki.Bundle, error) {
	if s == nil || s.root == "" {
		return nil, ErrNoWikis
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lib != nil {
		return s.lib, nil
	}
	b, err := wiki.Open(ctx, filepath.Join(s.root, "library"), wiki.Options{
		Layout: wiki.LibraryLayout,
		Git:    s.git,
		Human:  s.human(),
	})
	if err != nil {
		return nil, err
	}
	s.lib = b
	return b, nil
}

// personalMemory opens the personal memory's bundle (design.md 5.16), in
// personal/ beside the projects' wikis, and picks up what was edited in it
// outside Veyloom.
func (s *wikiShelf) personalMemory(ctx context.Context) (*wiki.Bundle, error) {
	if s == nil || s.root == "" {
		return nil, ErrNoWikis
	}
	s.mu.Lock()
	b := s.personal
	if b == nil {
		var err error
		b, err = wiki.Open(ctx, filepath.Join(s.root, "personal"), wiki.Options{
			Layout: wiki.PersonalLayout,
			Git:    s.git,
			Human:  s.human(),
		})
		if err != nil {
			s.mu.Unlock()
			return nil, err
		}
		s.personal = b
	}
	s.mu.Unlock()
	if _, err := b.Sync(ctx); err != nil {
		s.logger.Warn("pick up edits to the personal memory", "err", err)
	}
	return b, nil
}

// human is the account as an OKF actor, or "" when there is none.
func (s *wikiShelf) human() string {
	if s.person == nil {
		return ""
	}
	return humanActor(s.person())
}

// archive puts a deleted project's wiki aside, under archive/, rather than
// deleting what the team learned: a new project of the same name would
// otherwise find it again in the same folder.
func (s *wikiShelf) archive(projectID, slug string, now time.Time) error {
	if s == nil || s.root == "" || slug == "" {
		return nil
	}
	s.mu.Lock()
	delete(s.bundles, projectID)
	s.mu.Unlock()
	from := filepath.Join(s.root, "projects", slug)
	if _, err := os.Stat(from); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	to := filepath.Join(s.root, "archive", "projects", slug+"-"+now.UTC().Format("20060102-150405"))
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	return os.Rename(from, to)
}

// humanActor turns a person's name into an OKF actor, human:<name>.
func humanActor(name string) string {
	name = strings.Join(strings.Fields(name), "-")
	if name == "" {
		return ""
	}
	return okf.Human(name)
}

// agentActor is how a member's writes are signed: the runtime and the model
// it ran, like claude-code/claude-sonnet-5 (OKF §7).
func agentActor(runtime, model string) string {
	producer := runtime
	if producer == "claude" {
		producer = "claude-code"
	}
	if producer == "" {
		producer = "agent"
	}
	model = strings.Join(strings.Fields(model), "-")
	if model == "" {
		model = "default"
	}
	return okf.Agent(producer, model)
}

// ArchiveProjectWiki puts a deleted project's wiki aside, and leaves the
// skills its team owned to nobody. The API calls it once the project is
// gone.
func (h *Hub) ArchiveProjectWiki(projectID, slug string) error {
	if err := h.wikis.archive(projectID, slug, h.now()); err != nil {
		return err
	}
	if h.cfg.WikiDir == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), h.cfg.StoreTimeout)
	defer cancel()
	return h.orphanSkills(ctx, slug)
}

// personActor names a person as an OKF actor, human:<name>.
func (h *Hub) personActor(ctx context.Context, userID string) (string, error) {
	user, err := h.store.GetUser(ctx, userID)
	if err != nil {
		return "", err
	}
	if actor := humanActor(user.Name); actor != "" {
		return actor, nil
	}
	return okf.Human(userID), nil
}
