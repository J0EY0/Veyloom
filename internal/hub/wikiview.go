package hub

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// The project wiki as people see it and change it (docs/design.md 5.14,
// step 4). What the UI reads is what is on disk: edits made in an editor
// are picked up first. What a person changes is committed under their
// name, and the room is told.

// WikiPageInfo is a page as lists show it.
type WikiPageInfo struct {
	Path        string   `json:"path"`
	Type        string   `json:"type"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags"`
	// Status is draft, stable or deprecated; Tier how far it was checked:
	// unverified, machine-confirmed or human-reviewed.
	Status      string     `json:"status"`
	Tier        string     `json:"tier"`
	GeneratedBy string     `json:"generated_by,omitempty"`
	GeneratedAt *time.Time `json:"generated_at,omitempty"`
	VerifiedAt  *time.Time `json:"verified_at,omitempty"`
	// VouchedAt is when a person last stood behind the page as it is:
	// wrote it, or confirmed it since it was last written.
	VouchedAt  *time.Time `json:"vouched_at,omitempty"`
	StaleAfter *time.Time `json:"stale_after,omitempty"`
	Stale      bool       `json:"stale,omitempty"`
	Modified   time.Time  `json:"modified"`
	// Resident says every turn carries the page: tagged resident, and
	// vouched for.
	Resident bool `json:"resident"`
	// Team is the project that owns a skill, by its wiki folder name;
	// empty for other pages and for a skill nobody owns.
	Team string `json:"team,omitempty"`
	// OnTrial says a skill of the library is on trial after an agent
	// changed it (design.md 5.15), for the list of skills to show.
	OnTrial bool `json:"on_trial,omitempty"`
	// Mount names the bundle the project mounts that the page is in; empty
	// for the wiki's own. Such a page is read-only.
	Mount string `json:"mount,omitempty"`
	// CheckedAt is when the page was last written or confirmed, whichever
	// is later; Review, when it is due to be checked again and why
	// (design.md 5.16). A project's own pages only.
	CheckedAt *time.Time  `json:"checked_at,omitempty"`
	Review    *WikiReview `json:"review,omitempty"`
}

func pageInfo(s wiki.Summary, now time.Time) WikiPageInfo {
	tags := s.Tags
	if tags == nil {
		tags = []string{}
	}
	return WikiPageInfo{
		Path: s.Path, Type: s.Type, Title: s.Title, Description: s.Description, Tags: tags,
		Status: string(s.Status), Tier: s.Tier.String(),
		GeneratedBy: s.Generated.By, GeneratedAt: optionalTime(s.Generated.At), VerifiedAt: optionalTime(s.Verified),
		VouchedAt: optionalTime(s.VouchedAt), StaleAfter: optionalTime(s.StaleAfter), Stale: s.Stale(now),
		Modified: s.Modified, Resident: s.Carried(), Team: s.Team,
	}
}

func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// WikiDir is where one type of page goes.
type WikiDir struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// WikiCatalog is a project's wiki at a glance.
type WikiCatalog struct {
	Pages []WikiPageInfo `json:"pages"`
	// Dirs are the types of page and where each goes, in the order the
	// wiki lists them.
	Dirs []WikiDir `json:"dirs"`
	// Folder is the wiki on disk, for a person who would rather edit it
	// in their editor.
	Folder string `json:"folder"`
	// History says whether changes can be looked back on and undone.
	History bool `json:"history"`
	// Teams are the projects owning the skill library's skills, by the
	// folder name their skills give; a team no project has any more is
	// missing.
	Teams []WikiTeam `json:"teams,omitempty"`
	// Mounts are the bundles a project's wiki mounts, read-only, with their
	// pages at the paths Veyloom gives them.
	Mounts []WikiMount `json:"mounts,omitempty"`
}

// WikiMount is one bundle a project's wiki mounts.
type WikiMount struct {
	Name   string         `json:"name"`
	Folder string         `json:"folder"`
	Pages  []WikiPageInfo `json:"pages"`
	// Error says why the bundle cannot be read; there are no pages then.
	// ErrorCode and ErrorParams name it for the web client, as an error
	// response's code and params do (store.Problem).
	Error       string       `json:"error,omitempty"`
	ErrorCode   string       `json:"error_code,omitempty"`
	ErrorParams store.Params `json:"error_params,omitempty"`
}

// WikiTeam is a project as the owner of skills.
type WikiTeam struct {
	Slug      string `json:"slug"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	RoomID    string `json:"room_id"`
}

// WikiLink is a page another one links to or from.
type WikiLink struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}

// WikiStamp is who confirmed a page, and when.
type WikiStamp struct {
	By string    `json:"by"`
	At time.Time `json:"at"`
}

// WikiSource is what a page rests on, and where it leads: the hub resolves
// its own veyloom:// sources to the topic they name.
type WikiSource struct {
	ID       string `json:"id,omitempty"`
	Resource string `json:"resource"`
	Title    string `json:"title,omitempty"`
	// A topic of the chat, and the turn in it when the source is one.
	RoomID      string `json:"room_id,omitempty"`
	ThreadID    string `json:"thread_id,omitempty"`
	TopicNumber int    `json:"topic_number,omitempty"`
	TurnID      string `json:"turn_id,omitempty"`
	// Or the message a file kept in the wiki came with: who sent it and
	// when, and its topic above unless it was said in the chat itself.
	MessageID string     `json:"message_id,omitempty"`
	SentBy    string     `json:"sent_by,omitempty"`
	SentAt    *time.Time `json:"sent_at,omitempty"`
	// Or another page of the wiki.
	Page string `json:"page,omitempty"`
}

// WikiPageView is one page in full.
type WikiPageView struct {
	WikiPageInfo
	// Body is the page's markdown, its links to other pages rewritten to
	// paths from the wiki's root (see rootLinks).
	Body string `json:"body"`
	// Hash identifies the page as read: a change made from it fails once
	// the page has changed since.
	Hash string `json:"hash"`
	// File is the page on disk.
	File      string       `json:"file"`
	Verified  []WikiStamp  `json:"verified"`
	Sources   []WikiSource `json:"sources"`
	Backlinks []WikiLink   `json:"backlinks"`
	// Installed names the agents a skill of the library is installed for
	// (design.md 5.15); nil for any other page.
	Installed []store.AgentRef `json:"installed,omitempty"`
	// Trial is a skill's latest trial, open or how it ended; nil for a
	// skill never changed by an agent, and for any other page.
	Trial *TrialView `json:"trial,omitempty"`
}

// TrialView is a skill's trial as its page shows it (design.md 5.15).
type TrialView struct {
	store.SkillTrial
	// Uses and Failed count, while the trial is open, the turns that used
	// the skill since its last change by how they ended; Needed is how
	// many that ended well keep it.
	Uses   int `json:"uses"`
	Failed int `json:"failed"`
	Needed int `json:"needed"`
	// Where the last change was made, while its turn is there.
	RoomID      string `json:"room_id,omitempty"`
	ThreadID    string `json:"thread_id,omitempty"`
	TopicNumber int    `json:"topic_number,omitempty"`
}

// WikiHit is one search result.
type WikiHit struct {
	WikiPageInfo
	Snippet string `json:"snippet,omitempty"`
}

// WikiCommit is one change in the wiki's history.
type WikiCommit struct {
	SHA     string           `json:"sha"`
	Author  string           `json:"author"`
	At      time.Time        `json:"at"`
	Subject string           `json:"subject"`
	Changes []WikiChangeLine `json:"changes"`
	// Where it came from, when a turn wrote it: who, in which topic.
	Member      string `json:"member,omitempty"`
	TurnID      string `json:"turn_id,omitempty"`
	RoomID      string `json:"room_id,omitempty"`
	ThreadID    string `json:"thread_id,omitempty"`
	TopicNumber int    `json:"topic_number,omitempty"`
	// ProjectName is the project a turn that wrote to the skill library
	// was in.
	ProjectName string `json:"project_name,omitempty"`
	// Undoable says the commit changed pages, which undoing it takes back.
	Undoable bool `json:"undoable"`
}

// WikiChangeLine is what a commit did to one page.
type WikiChangeLine struct {
	Kind  string `json:"kind"`
	Path  string `json:"path,omitempty"`
	Title string `json:"title,omitempty"`
	Text  string `json:"text"`
}

// rootLinks rewrites the links of a page at p that lead to other pages of
// the wiki to paths from its root. The page is read somewhere other than
// its folder, in a browser under the chat's address, where a link like
// ../facts/x.md would lead somewhere else.
func rootLinks(p, body string) string {
	return okf.RewriteLinks(body, func(target string) (string, bool) {
		to, ok := okf.Resolve(p, target)
		if !ok || path.Ext(to) != ".md" {
			return "", false
		}
		to = (&url.URL{Path: to}).EscapedPath()
		return to, to != target
	})
}

// wikiRef is one wiki as the UI reads it: a project's, or the skill
// library, whose project is the zero one.
type wikiRef struct {
	scope   store.WikiScope
	project store.Project
	bundle  *wiki.Bundle
}

// openWiki opens a project's wiki as it is on disk now.
func (h *Hub) openWiki(ctx context.Context, projectID string) (wikiRef, error) {
	project, err := h.store.GetProject(ctx, projectID)
	if err != nil {
		return wikiRef{}, err
	}
	return h.openProjectWiki(ctx, project)
}

// openLibrary opens the skill library as it is on disk now.
func (h *Hub) openLibrary(ctx context.Context) (wikiRef, error) {
	b, err := h.wikis.library(ctx)
	if err != nil {
		return wikiRef{}, err
	}
	h.wikis.sync(ctx, b, "", "")
	return wikiRef{scope: store.WikiLibrary, bundle: b}, nil
}

// changed tells the chat of a project that its wiki changed. The library
// has no chat of its own; its page reads it again by itself.
func (h *Hub) changed(r wikiRef) {
	if r.scope == store.WikiProject {
		h.wikis.notify(r.project.ID, r.project.MainRoomID)
	}
}

// WikiCatalog lists a project's wiki: every page, and where each type goes,
// and the pages of the bundles it mounts.
func (h *Hub) WikiCatalog(ctx context.Context, projectID string) (WikiCatalog, error) {
	r, err := h.openWiki(ctx, projectID)
	if err != nil {
		return WikiCatalog{}, err
	}
	catalog := h.catalog(r, wiki.ProjectLayout)
	now := h.now()
	h.markReviews(ctx, r, r.bundle.Pages(), catalog.Pages)
	for _, m := range h.wikis.mounts(ctx, r.project) {
		mount := WikiMount{Name: m.name, Folder: m.dir, Pages: []WikiPageInfo{}}
		if m.err != nil {
			mount.Error = store.Reason(m.err)
			var p *store.Problem
			if errors.As(m.err, &p) {
				mount.ErrorCode, mount.ErrorParams = p.Code, p.Params
			}
		} else {
			for _, s := range m.bundle.Pages() {
				mount.Pages = append(mount.Pages, mountedInfo(m.name, s, now))
			}
		}
		catalog.Mounts = append(catalog.Mounts, mount)
	}
	return catalog, nil
}

// markReviews notes on a project's own pages when each was last checked
// and which are due to be checked again. What cannot be worked out costs
// the marks, not the listing.
func (h *Hub) markReviews(ctx context.Context, r wikiRef, pages []wiki.Summary, infos []WikiPageInfo) {
	markReviews(ctx, h.store, h.logger, r, pages, infos, h.now())
}

func markReviews(ctx context.Context, st reviewStore, logger *slog.Logger, r wikiRef, pages []wiki.Summary, infos []WikiPageInfo, now time.Time) {
	if r.scope != store.WikiProject {
		return
	}
	reviews, err := wikiReviews(ctx, st, r.project.ID, pages, now)
	if err != nil {
		logger.Warn("find the wiki pages to check again", "project", r.project.ID, "err", err)
	}
	checked := make(map[string]time.Time, len(pages))
	for _, p := range pages {
		checked[p.Path] = p.CheckedAt()
	}
	for i := range infos {
		info := &infos[i]
		if at, ok := checked[info.Path]; ok && info.Mount == "" {
			info.CheckedAt = optionalTime(at)
		}
		if review, ok := reviews[info.Path]; ok && info.Mount == "" {
			info.Review = &review
		}
	}
}

// mountedInfo is a page of a mount as lists show it.
func mountedInfo(name string, s wiki.Summary, now time.Time) WikiPageInfo {
	info := pageInfo(s, now)
	info.Path, info.Mount, info.Resident = mountPath(name, s.Path), name, false
	return info
}

// LibraryCatalog lists the skill library: its patterns and skills, which
// of the skills are on trial, and the teams owning them.
func (h *Hub) LibraryCatalog(ctx context.Context) (WikiCatalog, error) {
	r, err := h.openLibrary(ctx)
	if err != nil {
		return WikiCatalog{}, err
	}
	catalog := h.catalog(r, wiki.LibraryLayout)
	seen := map[string]bool{}
	skills := map[string]int{}
	for i, p := range catalog.Pages {
		if name := wiki.SkillName(p.Path); name != "" {
			skills[name] = i
		}
		if p.Team == "" || seen[p.Team] {
			continue
		}
		seen[p.Team] = true
		if project, err := h.store.GetProjectBySlug(ctx, p.Team); err == nil {
			catalog.Teams = append(catalog.Teams, WikiTeam{Slug: p.Team, ProjectID: project.ID, Name: project.Name, RoomID: project.MainRoomID})
		}
	}
	names := make([]string, 0, len(skills))
	for name := range skills {
		names = append(names, name)
	}
	trials, err := h.store.ListOpenSkillTrials(ctx, names)
	if err != nil {
		return WikiCatalog{}, err
	}
	for _, trial := range trials {
		if i, ok := skills[trial.Skill]; ok {
			catalog.Pages[i].OnTrial = true
		}
	}
	return catalog, nil
}

func (h *Hub) catalog(r wikiRef, layout wiki.Layout) WikiCatalog {
	now := h.now()
	pages := r.bundle.Pages()
	out := WikiCatalog{Pages: make([]WikiPageInfo, 0, len(pages)), Folder: r.bundle.Dir(), History: r.bundle.KeepsHistory()}
	for _, s := range pages {
		out.Pages = append(out.Pages, pageInfo(s, now))
	}
	for _, d := range layout.Dirs {
		out.Dirs = append(out.Dirs, WikiDir{Name: d.Name, Type: d.Type})
	}
	return out
}

// WikiPage reads one page of a project's wiki in full, or of a bundle it
// mounts.
// WikiFile reads a file the project's wiki keeps beside its pages, one
// people sent that a page keeps whole (docs/design.md 5.16).
func (h *Hub) WikiFile(ctx context.Context, projectID, path string) ([]byte, error) {
	r, err := h.openWiki(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return r.bundle.Asset(path)
}

func (h *Hub) WikiPage(ctx context.Context, projectID, path string) (WikiPageView, error) {
	r, err := h.openWiki(ctx, projectID)
	if err != nil {
		return WikiPageView{}, err
	}
	if name, inner, ok := splitMount(path); ok {
		return h.mountedView(ctx, r, name, inner)
	}
	return h.pageView(ctx, r, path)
}

// mountedView reads a page of a bundle the project mounts: as a page of
// its own reads, with nothing a person could change on it.
func (h *Hub) mountedView(ctx context.Context, r wikiRef, name, inner string) (WikiPageView, error) {
	mount, err := mountNamed(h.wikis.mounts(ctx, r.project), name)
	if err != nil {
		return WikiPageView{}, err
	}
	view, err := h.pageView(ctx, wikiRef{scope: r.scope, project: r.project, bundle: mount.bundle}, inner)
	if err != nil {
		return WikiPageView{}, err
	}
	page, err := mount.bundle.Page(inner)
	if err != nil {
		return WikiPageView{}, err
	}
	view.WikiPageInfo = mountedInfo(name, page.Summary, h.now())
	view.Body = mountLinks(name, page.Path, page.Doc.Body())
	for i, s := range view.Sources {
		if s.Page != "" {
			view.Sources[i].Page = mountPath(name, s.Page)
		}
	}
	for i, l := range view.Backlinks {
		view.Backlinks[i].Path = mountPath(name, l.Path)
	}
	return view, nil
}

// LibraryPage reads one page of the skill library in full; a skill's says
// which agents it is installed for.
func (h *Hub) LibraryPage(ctx context.Context, path string) (WikiPageView, error) {
	r, err := h.openLibrary(ctx)
	if err != nil {
		return WikiPageView{}, err
	}
	view, err := h.pageView(ctx, r, path)
	if err != nil {
		return WikiPageView{}, err
	}
	if name := wiki.SkillName(view.Path); name != "" {
		if view.Installed, err = h.store.ListSkillAgents(ctx, name); err != nil {
			return WikiPageView{}, err
		}
		if view.Trial, err = h.trialView(ctx, name); err != nil {
			return WikiPageView{}, err
		}
	}
	return view, nil
}

// InstallSkill installs a skill of the library for an agent, or takes it
// off (design.md 5.15): the agent's turns are given the skills installed
// for it and no others. It answers with the agents the skill is installed
// for now. A skill gone from the library can still be taken off.
func (h *Hub) InstallSkill(ctx context.Context, name, agentID string, installed bool) ([]store.AgentRef, error) {
	if installed {
		if err := h.CheckSkills(ctx, []string{name}); err != nil {
			return nil, err
		}
	}
	if err := h.store.SetAgentSkill(ctx, agentID, name, installed); err != nil {
		return nil, err
	}
	return h.store.ListSkillAgents(ctx, name)
}

// CheckSkills refuses a name that is no current skill of the library, one
// it never had or has retired: what can be installed for an agent.
func (h *Hub) CheckSkills(ctx context.Context, names []string) error {
	if len(names) == 0 {
		return nil
	}
	r, err := h.openLibrary(ctx)
	if err != nil {
		return err
	}
	for _, name := range names {
		if !wikiSlug(name) {
			return store.Missing("skillUnknown", store.Params{"name": name}, "the skill library has no skill %s", name)
		}
		if s, err := r.bundle.Page(wiki.SkillPath(name)); err != nil || s.Type != "Skill" || s.Status == okf.Deprecated {
			return store.Missing("skillUnknown", store.Params{"name": name}, "the skill library has no skill %s", name)
		}
	}
	return nil
}

func (h *Hub) pageView(ctx context.Context, r wikiRef, path string) (WikiPageView, error) {
	page, err := r.bundle.Page(path)
	if err != nil {
		return WikiPageView{}, err
	}
	file, _ := r.bundle.File(page.Path)
	view := WikiPageView{
		WikiPageInfo: pageInfo(page.Summary, h.now()),
		Body:         rootLinks(page.Path, page.Doc.Body()),
		Hash:         page.Hash,
		File:         file,
		Verified:     []WikiStamp{},
		Sources:      []WikiSource{},
		Backlinks:    []WikiLink{},
	}
	infos := []WikiPageInfo{view.WikiPageInfo}
	h.markReviews(ctx, r, []wiki.Summary{page.Summary}, infos)
	view.WikiPageInfo = infos[0]
	for _, v := range page.Doc.Verified() {
		view.Verified = append(view.Verified, WikiStamp{By: v.By, At: v.At})
	}
	for _, s := range page.Doc.Sources() {
		view.Sources = append(view.Sources, h.wikiSource(ctx, r.bundle, s))
	}
	for _, p := range r.bundle.Backlinks(page.Path) {
		link := WikiLink{Path: p, Title: p}
		if from, err := r.bundle.Page(p); err == nil {
			link.Title = from.Title
		}
		view.Backlinks = append(view.Backlinks, link)
	}
	return view, nil
}

// wikiSource finds where a source leads. What the hub wrote itself,
// veyloom://turns/<id> and veyloom://rooms/<room>/topics/<n>, leads to a
// topic of the chat, and veyloom://rooms/<room>/messages/<id>, the message
// a kept file came with, to its sender and topic; a path from the wiki's
// root to another page.
func (h *Hub) wikiSource(ctx context.Context, b *wiki.Bundle, s okf.Source) WikiSource {
	out := WikiSource{ID: s.ID, Resource: s.Resource, Title: s.Title}
	switch rest, ok := strings.CutPrefix(s.Resource, "veyloom://"); {
	case ok && strings.HasPrefix(rest, "turns/"):
		if turn, err := h.store.GetTurn(ctx, strings.TrimPrefix(rest, "turns/")); err == nil {
			out.TurnID, out.RoomID, out.ThreadID = turn.ID, turn.RoomID, turn.ThreadID
			if thread, err := h.store.GetThread(ctx, turn.ThreadID); err == nil {
				out.TopicNumber = thread.Number
			}
		}
	case ok && strings.HasPrefix(rest, "rooms/"):
		parts := strings.Split(rest, "/")
		if len(parts) != 4 {
			break
		}
		switch parts[2] {
		case "topics":
			if n, err := strconv.Atoi(parts[3]); err == nil {
				if thread, err := h.store.ThreadByNumber(ctx, parts[1], n); err == nil {
					out.RoomID, out.ThreadID, out.TopicNumber = thread.RoomID, thread.ID, thread.Number
				}
			}
		case "messages":
			h.messageSource(ctx, &out, parts[3])
		}
	case strings.HasPrefix(s.Resource, "/"):
		if page, err := b.Page(s.Resource); err == nil {
			out.Page = page.Path
		}
	}
	return out
}

// messageSource fills in the message a kept file came with. A message gone
// since leaves the source as written.
func (h *Hub) messageSource(ctx context.Context, out *WikiSource, id string) {
	msg, err := h.store.GetMessage(ctx, id)
	if err != nil {
		return
	}
	at := msg.CreatedAt
	out.MessageID, out.RoomID, out.SentAt = msg.ID, msg.Room, &at
	switch msg.SenderKind {
	case store.SenderUser:
		if user, err := h.store.GetUser(ctx, msg.UserID); err == nil {
			out.SentBy = user.Name
		}
	case store.SenderAgent:
		if member, err := h.store.GetMember(ctx, msg.MemberID); err == nil {
			out.SentBy = member.DisplayName
		}
	}
	if thread, err := h.store.ThreadOfMessage(ctx, msg.ID); err == nil {
		out.ThreadID, out.TopicNumber = thread.ID, thread.Number
	}
}

// SearchWiki finds pages of a project's wiki by their text, best first.
func (h *Hub) SearchWiki(ctx context.Context, projectID, query string, limit int) ([]WikiHit, error) {
	r, err := h.openWiki(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return h.search(ctx, r, query, limit), nil
}

// SearchLibrary finds pages of the skill library by their text.
func (h *Hub) SearchLibrary(ctx context.Context, query string, limit int) ([]WikiHit, error) {
	r, err := h.openLibrary(ctx)
	if err != nil {
		return nil, err
	}
	return h.search(ctx, r, query, limit), nil
}

func (h *Hub) search(ctx context.Context, r wikiRef, query string, limit int) []WikiHit {
	now := h.now()
	hits := searchMounted(r.bundle, h.wikis.mounts(ctx, r.project), query, limit)
	out := make([]WikiHit, 0, len(hits))
	for _, hit := range hits {
		info := pageInfo(hit.Summary, now)
		if name, _, ok := splitMount(hit.Path); ok {
			info.Mount, info.Resident = name, false
		}
		out = append(out, WikiHit{WikiPageInfo: info, Snippet: hit.Snippet})
	}
	return out
}

// WikiHistory lists the latest changes to a page of a project's wiki, or to
// the whole wiki when path is empty, newest first.
func (h *Hub) WikiHistory(ctx context.Context, projectID, path string, limit int) ([]WikiCommit, error) {
	r, err := h.openWiki(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if _, _, ok := splitMount(path); ok {
		// A mounted bundle's history is its own, not the wiki's.
		return []WikiCommit{}, nil
	}
	return h.history(ctx, r, path, limit)
}

// LibraryHistory is WikiHistory for the skill library.
func (h *Hub) LibraryHistory(ctx context.Context, path string, limit int) ([]WikiCommit, error) {
	r, err := h.openLibrary(ctx)
	if err != nil {
		return nil, err
	}
	return h.history(ctx, r, path, limit)
}

func (h *Hub) history(ctx context.Context, r wikiRef, path string, limit int) ([]WikiCommit, error) {
	commits, err := r.bundle.History(ctx, path, limit)
	if err != nil {
		return nil, err
	}
	topics := map[string]store.Thread{}
	projects := map[string]string{}
	out := make([]WikiCommit, 0, len(commits))
	for _, c := range commits {
		wc := WikiCommit{
			SHA: c.SHA, Author: c.Author, At: c.At, Subject: c.Subject, Changes: []WikiChangeLine{},
			Member: c.Trailers["Veyloom-Member"], TurnID: c.Trailers["Veyloom-Turn"],
		}
		for _, ch := range c.Changes {
			wc.Changes = append(wc.Changes, WikiChangeLine{Kind: ch.Kind, Path: ch.Path, Title: ch.Title, Text: ch.Text})
			switch ch.Kind {
			case okf.LogCreation, okf.LogUpdate, okf.LogDeprecation, okf.LogRename, okf.LogVerification:
				wc.Undoable = true
			}
		}
		if wc.TurnID != "" {
			if turn, err := h.store.GetTurn(ctx, wc.TurnID); err == nil {
				thread, seen := topics[turn.ThreadID]
				if !seen {
					thread, _ = h.store.GetThread(ctx, turn.ThreadID)
					topics[turn.ThreadID] = thread
				}
				wc.RoomID, wc.ThreadID, wc.TopicNumber = turn.RoomID, turn.ThreadID, thread.Number
				// The library and the personal memory are every project's:
				// say whose chat it was.
				if r.scope != store.WikiProject {
					name, seen := projects[turn.RoomID]
					if !seen {
						if project, err := h.store.RoomProject(ctx, turn.RoomID); err == nil {
							name = project.Name
						}
						projects[turn.RoomID] = name
					}
					wc.ProjectName = name
				}
			}
		}
		out = append(out, wc)
	}
	return out, nil
}

// RevertWiki undoes one change to a project's wiki as the person, and says
// so in the log. A later change to the same lines makes it ErrConflict.
func (h *Hub) RevertWiki(ctx context.Context, projectID, sha, userID, reason string) (string, error) {
	r, err := h.openWiki(ctx, projectID)
	if err != nil {
		return "", err
	}
	return h.revert(ctx, r, sha, userID, reason)
}

// RevertLibrary is RevertWiki for the skill library.
func (h *Hub) RevertLibrary(ctx context.Context, sha, userID, reason string) (string, error) {
	r, err := h.openLibrary(ctx)
	if err != nil {
		return "", err
	}
	return h.revert(ctx, r, sha, userID, reason)
}

func (h *Hub) revert(ctx context.Context, r wikiRef, sha, userID, reason string) (string, error) {
	person, err := h.personActor(ctx, userID)
	if err != nil {
		return "", err
	}
	undo, err := r.bundle.Revert(ctx, sha, person, reason)
	if errors.Is(err, wiki.ErrNoHistory) {
		return "", fmt.Errorf("%w: this wiki keeps no history to undo", store.ErrInvalidInput)
	}
	if err != nil {
		return "", err
	}
	h.changed(r)
	return undo, nil
}

// VerifyWikiPage records that the person confirmed a page of a project's
// wiki as it is: it becomes human-reviewed, and a resident page is carried
// from now on.
func (h *Hub) VerifyWikiPage(ctx context.Context, projectID, path, userID string) (WikiPageView, error) {
	if _, _, ok := splitMount(path); ok {
		return WikiPageView{}, readOnlyMount(path)
	}
	r, err := h.openWiki(ctx, projectID)
	if err != nil {
		return WikiPageView{}, err
	}
	return h.personWrites(ctx, r, path, userID, "Confirmed", verify)
}

// VerifyLibraryPage is VerifyWikiPage for the skill library.
func (h *Hub) VerifyLibraryPage(ctx context.Context, path, userID string) (WikiPageView, error) {
	r, err := h.openLibrary(ctx)
	if err != nil {
		return WikiPageView{}, err
	}
	view, err := h.personWrites(ctx, r, path, userID, "Confirmed", verify)
	if err != nil {
		return WikiPageView{}, err
	}
	// A person confirming a skill on trial keeps the change (design.md 5.15).
	if name := wiki.SkillName(view.Path); name != "" {
		person, err := h.personActor(ctx, userID)
		if err != nil {
			return WikiPageView{}, err
		}
		if err := h.turns.endTrialAsKept(ctx, name, person, ""); err != nil {
			return WikiPageView{}, err
		}
	}
	return h.LibraryPage(ctx, view.Path)
}

// RollbackSkill puts a skill on trial back the way it was before the
// change of an agent's, as the person and for reason, which may be empty
// (design.md 5.15).
func (h *Hub) RollbackSkill(ctx context.Context, name, userID, reason string) (WikiPageView, error) {
	person, err := h.personActor(ctx, userID)
	if err != nil {
		return WikiPageView{}, err
	}
	if _, err := h.openLibrary(ctx); err != nil {
		return WikiPageView{}, err
	}
	if _, err := h.turns.rollbackSkill(ctx, name, person, reason); err != nil {
		return WikiPageView{}, err
	}
	return h.LibraryPage(ctx, wiki.SkillPath(name))
}

// trialView is the latest trial of the skill called name, nil when it was
// never on one.
func (h *Hub) trialView(ctx context.Context, name string) (*TrialView, error) {
	trial, err := h.store.LatestSkillTrial(ctx, name)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	view := &TrialView{SkillTrial: trial, Needed: h.cfg.SkillTrialUses}
	if trial.Status == store.TrialOpen {
		if view.Uses, view.Failed, err = h.store.SkillTrialUses(ctx, name, trial.ChangedAt); err != nil {
			return nil, err
		}
	}
	if trial.TurnID != "" {
		if turn, err := h.store.GetTurn(ctx, trial.TurnID); err == nil {
			view.RoomID, view.ThreadID = turn.RoomID, turn.ThreadID
			if thread, err := h.store.GetThread(ctx, turn.ThreadID); err == nil {
				view.TopicNumber = thread.Number
			}
		}
	}
	return view, nil
}

func verify(w *wiki.Writer, path string) error {
	_, err := w.Verify(path)
	return err
}

// SetWikiResident makes a page one that every turn carries in full, or no
// longer. Making it resident is the person's word for it as it is, so
// the page is confirmed too.
func (h *Hub) SetWikiResident(ctx context.Context, projectID, path string, resident bool, userID string) (WikiPageView, error) {
	if _, _, ok := splitMount(path); ok {
		return WikiPageView{}, readOnlyMount(path)
	}
	if p, err := wiki.CleanPath(path); err == nil && p == wiki.MemoryPath {
		return WikiPageView{}, fmt.Errorf("%w: every turn carries the project memory already", store.ErrInvalidInput)
	}
	r, err := h.openWiki(ctx, projectID)
	if err != nil {
		return WikiPageView{}, err
	}
	subject := "No longer resident"
	if resident {
		subject = "Resident"
	}
	return h.personWrites(ctx, r, path, userID, subject, func(w *wiki.Writer, path string) error {
		if _, err := w.Tag(path, wiki.ResidentTag, resident); err != nil || !resident {
			return err
		}
		_, err := w.Verify(path)
		return err
	})
}

// TransferSkill hands a skill over to the team of another project, which
// decides on its changes from then on (design.md 5.10). Where it came
// from stays in its sources and history.
func (h *Hub) TransferSkill(ctx context.Context, name, projectID, userID string) (WikiPageView, error) {
	project, err := h.store.GetProject(ctx, projectID)
	if err != nil {
		return WikiPageView{}, err
	}
	r, err := h.openLibrary(ctx)
	if err != nil {
		return WikiPageView{}, err
	}
	view, err := h.personWrites(ctx, r, wiki.SkillPath(name), userID, "Handed over to "+project.WikiSlug, func(w *wiki.Writer, path string) error {
		_, err := w.SetMetadata(path, wiki.TeamKey, project.WikiSlug)
		return err
	})
	if err != nil {
		return WikiPageView{}, err
	}
	return h.LibraryPage(ctx, view.Path)
}

// orphanSkills leaves the skills of a team whose project is gone to
// nobody: they stay in use, marked unowned, until a person hands them to
// another team. A new project that happens to get the same folder name
// does not inherit them.
func (h *Hub) orphanSkills(ctx context.Context, slug string) error {
	b, err := h.wikis.library(ctx)
	if err != nil || slug == "" {
		return err
	}
	author := h.wikis.human()
	if author == "" {
		author = okf.Process("veyloom")
	}
	w, err := b.Writer(author)
	if err != nil {
		return err
	}
	for _, s := range b.Pages() {
		if s.Type == "Skill" && s.Team == slug {
			if _, err := w.SetMetadata(s.Path, wiki.TeamKey, ""); err != nil {
				return err
			}
		}
	}
	if len(w.Pending()) == 0 {
		return nil
	}
	_, err = w.Commit(ctx, "Left without a team: the project "+slug+" is gone")
	return err
}

// personWrites makes one change to a page as the person and commits it.
func (h *Hub) personWrites(ctx context.Context, r wikiRef, path, userID, subject string, write func(*wiki.Writer, string) error) (WikiPageView, error) {
	person, err := h.personActor(ctx, userID)
	if err != nil {
		return WikiPageView{}, err
	}
	page, err := r.bundle.Page(path)
	if err != nil {
		return WikiPageView{}, err
	}
	w, err := r.bundle.Writer(person)
	if err != nil {
		return WikiPageView{}, err
	}
	if err := write(w, page.Path); err != nil {
		return WikiPageView{}, err
	}
	if len(w.Pending()) > 0 {
		if _, err := w.Commit(ctx, subject+": "+page.Title); err != nil {
			return WikiPageView{}, err
		}
		h.changed(r)
	}
	return h.pageView(ctx, r, page.Path)
}

// SkillUses lists the turns that used a skill, newest first.
func (h *Hub) SkillUses(ctx context.Context, name string, limit int) ([]store.SkillUse, error) {
	return h.store.ListSkillUses(ctx, name, limit)
}
