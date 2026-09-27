package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// The wiki tools' answers (docs/design.md 5.5). A turn reaches the wiki of
// its own project and no other: the project comes from the turn's room,
// never from the call.

// Limits of a wiki tool's answer, which a model reads.
const (
	wikiSearchDefault = 10
	wikiSearchMax     = 30
)

// wikiArgs are the wiki tools' arguments; each tool reads its own.
type wikiArgs struct {
	Query       string      `json:"query"`
	Limit       int         `json:"limit"`
	Path        string      `json:"path"`
	Type        string      `json:"type"`
	Slug        string      `json:"slug"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Body        string      `json:"body"`
	Tags        []string    `json:"tags"`
	Sources     []okfSource `json:"sources"`
	Topics      []int       `json:"topics"`
	Edits       []wiki.Edit `json:"edits"`
	Successor   string      `json:"successor"`
	Reason      string      `json:"reason"`
	Scope       string      `json:"scope"`
	// Files are ids of files people sent, kept with a new page.
	Files []string `json:"files"`
	// related_wiki's: a path of the repository to start from, and how
	// many steps to go.
	File  string `json:"file"`
	Depth int    `json:"depth"`
}

type okfSource struct {
	ID       string `json:"id"`
	Resource string `json:"resource"`
	Title    string `json:"title"`
}

// turnWiki is a turn's hold on one wiki: its project's, the skill library
// every project shares, or the personal memory's bundle.
type turnWiki struct {
	scope store.WikiScope
	// project is the turn's project, whose wiki this is, or whose member
	// writes to the library.
	project store.Project
	bundle  *wiki.Bundle
	writer  *wiki.Writer
	author  string
}

// what names the wiki in what the agent is told.
func (tw *turnWiki) what() string {
	if tw.scope == store.WikiLibrary {
		return "the skill library"
	}
	return "this project's wiki"
}

// turnWiki opens one of the turn's wikis on its first use. Edits made
// outside Veyloom since the bundle was last looked at are picked up first,
// so the agent reads what is there.
func (m *TurnManager) turnWiki(ctx context.Context, at *activeTurn, scope store.WikiScope) (*turnWiki, error) {
	at.wikiMu.Lock()
	defer at.wikiMu.Unlock()
	if tw := at.wikis[scope]; tw != nil {
		return tw, nil
	}
	project, err := m.store.RoomProject(ctx, at.thread.RoomID)
	if err != nil {
		return nil, err
	}
	var b *wiki.Bundle
	switch scope {
	case store.WikiLibrary:
		if b, err = m.wikis.library(ctx); err == nil {
			m.wikis.sync(ctx, b, "", "")
		}
	case store.WikiPersonal:
		b, err = m.wikis.personalMemory(ctx)
	default:
		if b, err = m.wikis.project(ctx, project); err == nil {
			m.wikis.sync(ctx, b, project.ID, at.thread.RoomID)
		}
	}
	if err != nil {
		return nil, err
	}
	author := agentActor(at.agent.Runtime, at.spec.Model)
	w, err := b.Writer(author)
	if err != nil {
		return nil, err
	}
	tw := &turnWiki{scope: scope, project: project, bundle: b, writer: w, author: author}
	if at.wikis == nil {
		at.wikis = map[store.WikiScope]*turnWiki{}
	}
	at.wikis[scope] = tw
	return tw, nil
}

// commitWiki records what the turn wrote to each wiki as one commit,
// whatever became of the turn: the pages were written as it went. It
// returns the pages of the project's wiki the turn wrote.
func (m *TurnManager) commitWiki(ctx context.Context, at *activeTurn) []string {
	at.wikiMu.Lock()
	holds := make([]*turnWiki, 0, len(at.wikis))
	for _, tw := range at.wikis {
		holds = append(holds, tw)
	}
	at.wikiMu.Unlock()
	var pages []string
	for _, tw := range holds {
		pending := tw.writer.Pending()
		if len(pending) == 0 {
			continue
		}
		if tw.scope == store.WikiProject {
			pages = append(pages, pending...)
		}
		subject := fmt.Sprintf("%s in topic #%d", at.member.DisplayName, at.thread.Number)
		trailers := []wiki.Trailer{
			{Key: "Veyloom-Turn", Value: at.turn.ID},
			{Key: "Veyloom-Member", Value: at.member.DisplayName},
			{Key: "Veyloom-Topic", Value: strconv.Itoa(at.thread.Number)},
		}
		if tw.scope != store.WikiProject {
			// The library and the personal memory are every project's: say
			// which one this came from.
			subject = fmt.Sprintf("%s of %s in topic #%d", at.member.DisplayName, tw.project.WikiSlug, at.thread.Number)
			trailers = append(trailers, wiki.Trailer{Key: "Veyloom-Project", Value: tw.project.WikiSlug})
		}
		if _, err := tw.writer.Commit(ctx, subject, trailers...); err != nil {
			m.logger.Error("commit the turn's wiki changes", "turn", at.turn.ID, "scope", tw.scope, "err", err)
		}
		switch tw.scope {
		case store.WikiLibrary:
			m.wikis.notify("", at.thread.RoomID)
		case store.WikiProject:
			m.wikis.notify(tw.project.ID, at.thread.RoomID)
		}
	}
	return pages
}

// answerWiki answers a wiki tool call of a running turn.
func (m *TurnManager) answerWiki(ctx context.Context, at *activeTurn, q runtime.RoomQuery) (string, error) {
	var args wikiArgs
	if len(q.Args) > 0 {
		if err := json.Unmarshal(q.Args, &args); err != nil {
			return "", fmt.Errorf("bad arguments: %v", err)
		}
	}
	var scope store.WikiScope
	switch args.Scope {
	case "", runtime.WikiScopeProject:
		scope = store.WikiProject
	case runtime.WikiScopeLibrary:
		scope = store.WikiLibrary
	default:
		return "", fmt.Errorf("scope %q is neither project nor library", args.Scope)
	}
	tw, err := m.turnWiki(ctx, at, scope)
	if err != nil {
		return "", err
	}
	// A project's wiki is read with the bundles it mounts, and only read.
	var mounts []mountedWiki
	if scope == store.WikiProject {
		mounts = m.wikis.mounts(ctx, tw.project)
	}
	// What the chat's turns search for and read is kept for the
	// maintainer (lookups.go).
	switch q.Tool {
	case runtime.WikiToolSearch:
		text, hits, err := tw.search(args, mounts)
		if err == nil {
			m.noteLookup(ctx, at, tw, store.WikiLookup{Query: strings.TrimSpace(args.Query), Hits: hits})
		}
		return text, err
	case runtime.WikiToolRead:
		if name, inner, ok := splitMount(args.Path); ok && scope == store.WikiProject {
			text, err := readMounted(mounts, name, inner)
			if err == nil {
				m.noteLookup(ctx, at, tw, store.WikiLookup{Path: mountPrefix + name + inner})
			}
			return text, err
		}
		text, path, err := m.readWiki(ctx, tw, args)
		if err == nil {
			m.noteLookup(ctx, at, tw, store.WikiLookup{Path: path})
		}
		return text, err
	case runtime.WikiToolRelated:
		return m.relatedWiki(ctx, tw, args, mounts)
	}
	if _, _, ok := splitMount(args.Path); ok && scope == store.WikiProject {
		return "", readOnlyMount(args.Path)
	}
	if p, err := wiki.CleanPath(args.Path); err == nil && p == wiki.MemoryPath && scope == store.WikiProject {
		return "", errors.New("the project memory changes with remember and forget, entry by entry, not with this tool")
	}
	if scope == store.WikiLibrary {
		switch q.Tool {
		case runtime.WikiToolWrite:
			if len(args.Files) > 0 {
				return "", errors.New("files are kept in the project wiki only: write the page there, or cite the topic they were sent in")
			}
			return m.writeLibrary(ctx, at, tw, args)
		case runtime.WikiToolPatch:
			return m.patchLibrary(ctx, at, tw, args)
		case runtime.WikiToolDeprecate:
			return m.deprecateLibrary(ctx, at, tw, args)
		}
	}
	switch q.Tool {
	case runtime.WikiToolWrite:
		return m.writeWiki(ctx, at, tw, args)
	case runtime.WikiToolPatch:
		return m.patchWiki(ctx, at, tw, args)
	case runtime.WikiToolDeprecate:
		return m.deprecateWiki(ctx, at, tw, args)
	}
	return "", fmt.Errorf("unknown wiki tool %q", q.Tool)
}

// search answers search_wiki, and says how many pages matched the words
// as written.
func (tw *turnWiki) search(args wikiArgs, mounts []mountedWiki) (string, int, error) {
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return "", 0, errors.New("search for what? give some words as query")
	}
	limit := args.Limit
	if limit <= 0 {
		limit = wikiSearchDefault
	}
	limit = min(limit, wikiSearchMax)
	hits := searchMounted(tw.bundle, mounts, query, limit)
	if len(hits) == 0 {
		return tw.searchMissed(query, mounts, limit), 0, nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Pages matching %q, best first:\n", query)
	for _, h := range hits {
		sb.WriteString(pageLine(h.Summary, 0) + "\n")
		if h.Snippet != "" {
			fmt.Fprintf(&sb, "     %s\n", h.Snippet)
		}
	}
	sb.WriteString("(read a page with read_wiki)\n")
	return sb.String(), len(hits), nil
}

// searchMissed answers a search no page matches: the pages sharing words
// with the query when some do, and how to search again. Words are split at
// spaces only, so a question asked whole matches just a page holding it as
// written.
func (tw *turnWiki) searchMissed(query string, mounts []mountedWiki, limit int) string {
	again := "search again with fewer or other words, split by spaces: a name, a path, an error's text"
	near := nearMounted(tw.bundle, mounts, query, limit)
	if len(near) == 0 {
		return fmt.Sprintf("No page of %s matches %q. To find one, %s; related_wiki finds the pages about a file.", tw.what(), query, again)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "No page of %s holds %q as written. Pages sharing words with it, best first:\n", tw.what(), query)
	for _, h := range near {
		sb.WriteString(pageLine(h.Summary, 0) + "\n")
	}
	fmt.Fprintf(&sb, "(read a page with read_wiki, or %s)\n", again)
	return sb.String()
}

// pageLine is a page as lists show it: where it is, what it is called,
// what kind of page it is and what it is about, the description cut to
// description bytes when that is not zero.
func pageLine(s wiki.Summary, description int) string {
	line := fmt.Sprintf("%s: %s (%s)", s.Path, s.Title, pageState(s))
	if description > 0 {
		s.Description = excerpt(s.Description, description)
	}
	if s.Description != "" {
		line += " - " + s.Description
	}
	return line
}

// pageState says what kind of page it is and how far to trust it.
func pageState(s wiki.Summary) string {
	parts := []string{s.Type}
	switch s.Status {
	case okf.Deprecated:
		parts = append(parts, "deprecated, kept for history")
	case okf.Draft:
		parts = append(parts, "draft")
	}
	switch s.Tier {
	case okf.HumanReviewed:
		parts = append(parts, "confirmed by a person")
	case okf.MachineConfirmed:
		parts = append(parts, "confirmed by a process")
	}
	return strings.Join(parts, "; ")
}

// readWiki answers read_wiki with the page, and says which page it is, as
// the wiki names it.
func (m *TurnManager) readWiki(ctx context.Context, tw *turnWiki, args wikiArgs) (string, string, error) {
	page, err := tw.bundle.Page(args.Path)
	if errors.Is(err, store.ErrNotFound) {
		return "", "", fmt.Errorf("%s has no page %s; search_wiki finds the ones it has", tw.what(), args.Path)
	}
	if err != nil {
		return "", "", err
	}
	text, err := page.Doc.Bytes()
	if err != nil {
		return "", "", err
	}
	var sb strings.Builder
	sb.Write(text)
	if !strings.HasSuffix(sb.String(), "\n") {
		sb.WriteString("\n")
	}
	if from := tw.bundle.Backlinks(page.Path); len(from) > 0 {
		fmt.Fprintf(&sb, "\n(linked from: %s)\n", strings.Join(from, ", "))
	}
	if page.Team != "" {
		fmt.Fprintf(&sb, "(looked after by the team of project %s)\n", page.Team)
	}
	if name := wiki.SkillName(page.Path); tw.scope == store.WikiLibrary && name != "" {
		if trial, err := m.store.OpenSkillTrial(ctx, name); err == nil {
			fmt.Fprintf(&sb, "(on trial since %s changed it on %s: kept once %d turns have used it and ended well, or rolled back)\n",
				trial.ChangedBy, trial.ChangedAt.Local().Format("2006-01-02"), m.trialUses)
		}
	}
	return sb.String(), page.Path, nil
}

func (m *TurnManager) writeWiki(ctx context.Context, at *activeTurn, tw *turnWiki, args wikiArgs) (string, error) {
	dir, ok := wikiDir(args.Type)
	if !ok {
		if slices.Contains(runtime.LibraryPageTypes, args.Type) {
			return "", fmt.Errorf("a %s goes in the skill library: write it with scope library", args.Type)
		}
		return "", fmt.Errorf("type %q is not one the wiki holds: %s", args.Type, strings.Join(runtime.WikiPageTypes, ", "))
	}
	for name, v := range map[string]string{"slug": args.Slug, "title": args.Title, "description": args.Description, "body": args.Body} {
		if strings.TrimSpace(v) == "" {
			return "", fmt.Errorf("give the page a %s", name)
		}
	}
	path := "/" + dir + "/" + strings.TrimSpace(args.Slug) + ".md"
	if _, err := tw.bundle.Page(path); err == nil {
		return "", fmt.Errorf("there is a page at %s already; read it with read_wiki and change it with patch_wiki", path)
	}
	d := okf.New(args.Type)
	d.SetString(okf.KeyTitle, strings.TrimSpace(args.Title))
	d.SetString(okf.KeyDescription, strings.Join(strings.Fields(args.Description), " "))
	d.SetTags(args.Tags)
	for _, s := range args.Sources {
		d.AddSource(okf.Source{ID: s.ID, Resource: s.Resource, Title: s.Title})
	}
	for _, n := range args.Topics {
		if n > 0 {
			d.AddSource(okf.Source{ID: fmt.Sprintf("topic-%d", n), Resource: fmt.Sprintf("veyloom://rooms/%s/topics/%d", at.thread.RoomID, n), Title: fmt.Sprintf("Topic #%d", n)})
		}
	}
	d.AddSource(okf.Source{ID: "veyloom-turn", Resource: "veyloom://turns/" + at.turn.ID, Title: fmt.Sprintf("%s in topic #%d", at.member.DisplayName, at.thread.Number)})
	files, err := m.filesToKeep(ctx, at, tw, strings.TrimSpace(args.Slug), args.Files)
	if err != nil {
		return "", err
	}
	body := args.Body
	for i, f := range files {
		d.AddSource(okf.Source{ID: fmt.Sprintf("file-%d", i+1), Resource: fmt.Sprintf("veyloom://rooms/%s/messages/%s", f.RoomID, f.MessageID), Title: f.Filename})
		body = linkKept(body, f)
	}
	d.SetBody(body)

	if _, err := tw.writer.Create(path, d); err != nil {
		return "", err
	}
	for _, f := range files {
		if err := tw.writer.PutAsset(f.path, f.data); err != nil {
			return "", err
		}
	}
	saved := fmt.Sprintf("Saved %s", path)
	for _, f := range files {
		saved += ", " + f.path
	}
	return saved + ". It goes into the wiki's history as part of this turn when the turn ends.", nil
}

// keptFile is a file people sent, about to be kept with a page.
type keptFile struct {
	store.Attachment
	// path is where it goes in the wiki, data what it holds.
	path string
	data []byte
}

// filesToKeep reads the files a new page keeps (docs/design.md 5.16): sent
// in this chat, no bigger than the wiki keeps, each to a path of its own
// under /files/<slug>/. Nothing is written yet.
func (m *TurnManager) filesToKeep(ctx context.Context, at *activeTurn, tw *turnWiki, pageSlug string, ids []string) ([]keptFile, error) {
	var out []keptFile
	taken := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || slices.ContainsFunc(out, func(f keptFile) bool { return f.ID == id }) {
			continue
		}
		a, err := m.store.GetAttachment(ctx, id)
		if err != nil || a.RoomID != at.thread.RoomID || a.MessageID == "" {
			return nil, fmt.Errorf("this chat has no file %s: read_topic and read_room give the ids of the files people sent", id)
		}
		if a.Size > wiki.MaxAsset {
			return nil, fmt.Errorf("%s is %d MB, more than the %d MB a file kept in the wiki may be: cite the topic it was sent in instead", a.Filename, a.Size>>20, wiki.MaxAsset>>20)
		}
		data, err := os.ReadFile(filepath.Join(m.attachmentDir, filepath.FromSlash(a.Path)))
		if err != nil {
			return nil, fmt.Errorf("%s could not be read: %v", a.Filename, err)
		}
		p := tw.bundle.FreeAssetPath(pageSlug, a.Filename)
		for n := 2; taken[p]; n++ {
			p = tw.bundle.FreeAssetPath(pageSlug, fmt.Sprintf("%s-%d%s", strings.TrimSuffix(a.Filename, filepath.Ext(a.Filename)), n, filepath.Ext(a.Filename)))
		}
		taken[p] = true
		out = append(out, keptFile{Attachment: a, path: p, data: data})
	}
	return out, nil
}

// linkKept makes sure the page links a file it keeps: at the end, a
// picture shown in place, anything else as a link, unless the page links
// it already.
func linkKept(body string, f keptFile) string {
	if strings.Contains(body, "("+f.path+")") {
		return body
	}
	link := fmt.Sprintf("[%s](%s)", f.Filename, f.path)
	if strings.HasPrefix(f.MediaType, "image/") {
		link = "!" + link
	}
	return strings.TrimRight(body, "\n") + "\n\n" + link + "\n"
}

func (m *TurnManager) patchWiki(ctx context.Context, at *activeTurn, tw *turnWiki, args wikiArgs) (string, error) {
	page, err := tw.bundle.Page(args.Path)
	if errors.Is(err, store.ErrNotFound) {
		return "", fmt.Errorf("the wiki has no page %s; add one with write_wiki", args.Path)
	}
	if err != nil {
		return "", err
	}
	edited, hash, err := tw.bundle.Edited(page.Path, args.Edits)
	if err != nil {
		return "", err
	}
	if err := keepsConfirmations(page.Doc, edited); err != nil {
		return "", err
	}
	if _, err := tw.writer.Edit(page.Path, args.Edits, hash); err != nil {
		return "", err
	}
	if err := noteWhy(tw, page.Path, args.Reason); err != nil {
		return "", err
	}
	return fmt.Sprintf("Changed %s. It goes into the wiki's history as part of this turn when the turn ends.", page.Path), nil
}

// noteWhy puts an agent's reason for a change beside the page's line in
// the wiki's log, where people and later upkeeps read it.
func noteWhy(tw *turnWiki, path, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return nil
	}
	return tw.writer.Note(path, reason)
}

// keepsConfirmations refuses an agent's edit that changes who confirmed a
// page (design.md 5.16): an agent confirms a page with confirm_wiki, and
// only a person, in the UI, confirms it as a person.
func keepsConfirmations(before, after *okf.Document) error {
	same := func(a, b okf.Stamp) bool { return a.By == b.By && a.At.Equal(b.At) }
	if !slices.EqualFunc(before.Verified(), after.Verified(), same) {
		return errors.New("verified says who confirmed the page and when, and is not written by hand: leave it as it is; a page you checked and found right you confirm with confirm_wiki")
	}
	return nil
}

func (m *TurnManager) deprecateWiki(ctx context.Context, at *activeTurn, tw *turnWiki, args wikiArgs) (string, error) {
	page, err := tw.bundle.Page(args.Path)
	if errors.Is(err, store.ErrNotFound) {
		return "", fmt.Errorf("the wiki has no page %s", args.Path)
	}
	if err != nil {
		return "", err
	}
	if page.Status == okf.Deprecated {
		return "", fmt.Errorf("%s is deprecated already", page.Path)
	}
	successor := strings.TrimSpace(args.Successor)
	if successor != "" {
		next, err := tw.bundle.Page(successor)
		if err != nil || next.Path == page.Path || next.Status == okf.Deprecated {
			return "", fmt.Errorf("%s cannot take over from %s: it should be another page of the wiki that is still current", successor, page.Path)
		}
		successor = next.Path
	}
	if strings.TrimSpace(args.Reason) == "" {
		return "", errors.New("say why the page no longer holds, as reason")
	}
	if _, err := tw.writer.Deprecate(page.Path, successor, args.Reason); err != nil {
		return "", err
	}
	return fmt.Sprintf("Deprecated %s. It goes into the wiki's history as part of this turn when the turn ends.", page.Path), nil
}

// wikiDir is the directory a page of the given type goes in.
func wikiDir(typ string) (string, bool) {
	for _, d := range wiki.ProjectLayout.Dirs {
		if d.Type == typ {
			return d.Name, true
		}
	}
	return "", false
}
