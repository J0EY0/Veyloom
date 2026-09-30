package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/hub"
	"github.com/J0EY0/veyloom/internal/store"
)

// fakeWikis answers with what a test put in it and records what it was
// asked. The project "p404" is unknown.
type fakeWikis struct {
	pages map[string]hub.WikiPageView
	asked []string
	// retired are skills the library had; "nope" it never had.
	retired []string
}

func (f *fakeWikis) record(format string, args ...any) {
	f.asked = append(f.asked, fmt.Sprintf(format, args...))
}

func (f *fakeWikis) project(id string) error {
	if id == "p404" {
		return fmt.Errorf("project %s: %w", id, store.ErrNotFound)
	}
	return nil
}

func (f *fakeWikis) ArchiveProjectWiki(projectID, slug string) error { return nil }

func (f *fakeWikis) WikiCatalog(_ context.Context, projectID string) (hub.WikiCatalog, error) {
	if err := f.project(projectID); err != nil {
		return hub.WikiCatalog{}, err
	}
	var pages []hub.WikiPageInfo
	for _, p := range f.pages {
		pages = append(pages, p.WikiPageInfo)
	}
	return hub.WikiCatalog{Pages: pages, Dirs: []hub.WikiDir{{Name: "facts", Type: "Fact"}}, History: true}, nil
}

// The files every fake wiki keeps.
var fakeWikiFiles = map[string]string{
	"/files/arch/diagram.png": "PNG", "/files/arch/page.html": "<script>alert(1)</script>",
	"/files/arch/notes.txt": "notes", "/files/arch/checklist.markdown": "- [ ] tag",
}

func (f *fakeWikis) WikiFile(_ context.Context, projectID, path string) ([]byte, error) {
	if err := f.project(projectID); err != nil {
		return nil, err
	}
	data, ok := fakeWikiFiles[path]
	if !ok {
		return nil, fmt.Errorf("%w: no file %s", store.ErrNotFound, path)
	}
	return []byte(data), nil
}

// A file the wiki keeps is shown in place only when that is safe: a
// picture is, a page of HTML is downloaded instead.
func TestWikiFile(t *testing.T) {
	handler := NewHandler(Deps{Wikis: &fakeWikis{}})
	rec := do(t, handler, http.MethodGet, "/api/v1/projects/p1/wiki/file?path=/files/arch/diagram.png", "", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "PNG" || rec.Header().Get("Content-Type") != "image/png" ||
		!strings.HasPrefix(rec.Header().Get("Content-Disposition"), "inline") || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("a picture: %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}
	rec = do(t, handler, http.MethodGet, "/api/v1/projects/p1/wiki/file?path=/files/arch/page.html", "", nil)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment") || rec.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Errorf("a page of HTML: %d %v", rec.Code, rec.Header())
	}
	// Text reads in place; markdown, kept as .markdown, too, and saves as
	// the .md it was.
	rec = do(t, handler, http.MethodGet, "/api/v1/projects/p1/wiki/file?path=/files/arch/notes.txt", "", nil)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "inline") {
		t.Errorf("plain text: %d %v", rec.Code, rec.Header())
	}
	rec = do(t, handler, http.MethodGet, "/api/v1/projects/p1/wiki/file?path=/files/arch/checklist.markdown", "", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" ||
		rec.Header().Get("Content-Disposition") != "inline; filename=checklist.md" || rec.Body.String() != "- [ ] tag" {
		t.Errorf("markdown: %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/projects/p1/wiki/file?path=/files/none.png", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("no such file: %d", rec.Code)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/projects/p1/wiki/file", "", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("no path: %d", rec.Code)
	}
}

func (f *fakeWikis) WikiPage(_ context.Context, projectID, path string) (hub.WikiPageView, error) {
	if err := f.project(projectID); err != nil {
		return hub.WikiPageView{}, err
	}
	page, ok := f.pages[path]
	if !ok {
		return hub.WikiPageView{}, fmt.Errorf("%w: no page %s", store.ErrNotFound, path)
	}
	return page, nil
}

func (f *fakeWikis) SearchWiki(_ context.Context, projectID, query string, limit int) ([]hub.WikiHit, error) {
	f.record("search %s %q %d", projectID, query, limit)
	return []hub.WikiHit{{WikiPageInfo: f.pages["/facts/a.md"].WikiPageInfo, Snippet: "…a…"}}, nil
}

func (f *fakeWikis) WikiHistory(_ context.Context, projectID, path string, limit int) ([]hub.WikiCommit, error) {
	f.record("history %s %q %d", projectID, path, limit)
	return []hub.WikiCommit{{SHA: "abc", Undoable: true}}, nil
}

func (f *fakeWikis) RevertWiki(_ context.Context, projectID, sha, userID, reason string) (string, error) {
	f.record("revert %s %s by %s: %s", projectID, sha, userID, reason)
	if sha == "tangled" {
		return "", fmt.Errorf("%w: a later change touched the same lines", store.ErrConflict)
	}
	return "def", nil
}

func (f *fakeWikis) VerifyWikiPage(ctx context.Context, projectID, path, userID string) (hub.WikiPageView, error) {
	f.record("verify %s %s by %s", projectID, path, userID)
	return f.WikiPage(ctx, projectID, path)
}

func (f *fakeWikis) SetWikiResident(ctx context.Context, projectID, path string, resident bool, userID string) (hub.WikiPageView, error) {
	f.record("resident %s %s %v by %s", projectID, path, resident, userID)
	return f.WikiPage(ctx, projectID, path)
}

func (f *fakeWikis) WikiQuestion(_ context.Context, projectID string) (hub.WikiQuestion, error) {
	if err := f.project(projectID); err != nil {
		return hub.WikiQuestion{}, err
	}
	if projectID == "empty" {
		return hub.WikiQuestion{}, store.Conflicting("noOneToAsk", nil, "the project has no member to ask")
	}
	f.record("question %s", projectID)
	return hub.WikiQuestion{ThreadID: "t9", MemberID: "m1", MemberName: "Keeper"}, nil
}

func (f *fakeWikis) LibraryCatalog(context.Context) (hub.WikiCatalog, error) {
	return hub.WikiCatalog{Pages: []hub.WikiPageInfo{{Path: "/skills/go/SKILL.md", Type: "Skill", Team: "veyloom"}}, Teams: []hub.WikiTeam{{Slug: "veyloom", Name: "Veyloom"}}}, nil
}

func (f *fakeWikis) LibraryPage(_ context.Context, path string) (hub.WikiPageView, error) {
	if path != "/skills/go/SKILL.md" {
		return hub.WikiPageView{}, fmt.Errorf("%w: no page %s", store.ErrNotFound, path)
	}
	return hub.WikiPageView{WikiPageInfo: hub.WikiPageInfo{Path: path, Type: "Skill", Team: "veyloom"}, Body: "Use tables."}, nil
}

func (f *fakeWikis) SearchLibrary(_ context.Context, query string, limit int) ([]hub.WikiHit, error) {
	f.record("library search %q %d", query, limit)
	return nil, nil
}

func (f *fakeWikis) LibraryHistory(_ context.Context, path string, limit int) ([]hub.WikiCommit, error) {
	f.record("library history %q %d", path, limit)
	return []hub.WikiCommit{{SHA: "abc", ProjectName: "Veyloom"}}, nil
}

func (f *fakeWikis) RevertLibrary(_ context.Context, sha, userID, _ string) (string, error) {
	f.record("library revert %s by %s", sha, userID)
	return "def", nil
}

func (f *fakeWikis) VerifyLibraryPage(ctx context.Context, path, userID string) (hub.WikiPageView, error) {
	f.record("library verify %s by %s", path, userID)
	return f.LibraryPage(ctx, path)
}

func (f *fakeWikis) TransferSkill(ctx context.Context, name, projectID, userID string) (hub.WikiPageView, error) {
	f.record("transfer %s to %s by %s", name, projectID, userID)
	if projectID == "p404" {
		return hub.WikiPageView{}, fmt.Errorf("project %s: %w", projectID, store.ErrNotFound)
	}
	return f.LibraryPage(ctx, "/skills/"+name+"/SKILL.md")
}

func (f *fakeWikis) SkillUses(_ context.Context, name string, limit int) ([]store.SkillUse, error) {
	f.record("uses %s %d", name, limit)
	return []store.SkillUse{{TurnID: "x1", ProjectName: "Veyloom", TopicNumber: 3}}, nil
}

func (f *fakeWikis) BuiltinSkills() []hub.BuiltinSkill {
	return []hub.BuiltinSkill{{Name: "team-practices", Description: "How the members of a Veyloom team split, hand on, check and report back work."}}
}

func (f *fakeWikis) ImportSkill(ctx context.Context, folder, team, userID string) (hub.WikiPageView, error) {
	f.record("import %s for %q by %s", folder, team, userID)
	if folder == "relative" {
		return hub.WikiPageView{}, store.Invalid("folderNotAbsolute", store.Params{"path": folder}, "%s is not a full path", folder)
	}
	return f.LibraryPage(ctx, "/skills/go/SKILL.md")
}

func (f *fakeWikis) UpdateSkill(ctx context.Context, folder, userID string) (hub.WikiPageView, error) {
	f.record("update from %s by %s", folder, userID)
	if folder == "/on/trial" {
		return hub.WikiPageView{}, store.Conflicting("skillOnTrial", store.Params{"name": "go"}, "the skill go is on trial")
	}
	return f.LibraryPage(ctx, "/skills/go/SKILL.md")
}

func (f *fakeWikis) RetireSkill(ctx context.Context, name string, retired bool, userID string) (hub.WikiPageView, error) {
	f.record("retire %s %v by %s", name, retired, userID)
	return f.LibraryPage(ctx, "/skills/go/SKILL.md")
}

func (f *fakeWikis) DeleteSkill(_ context.Context, name, userID string) error {
	f.record("delete %s by %s", name, userID)
	if name == "busy" {
		return store.Conflicting("skillBusy", store.Params{"name": name}, "a turn is changing the skill %s", name)
	}
	return nil
}

func (f *fakeWikis) LocalSkills(context.Context) ([]hub.LocalSkill, error) {
	f.record("local skills")
	return []hub.LocalSkill{{Name: "pdf", Description: "Use with PDFs.", Folder: "/home/me/.agents/skills/pdf", Where: "~/.agents/skills"}}, nil
}

func (f *fakeWikis) UploadSkills(_ context.Context, up hub.SkillUpload) ([]hub.UploadedSkill, error) {
	if up.Zip != nil {
		f.record("upload zip %s (%d bytes) for %q by %s", up.Name, up.ZipSize, up.Team, up.UserID)
		return []hub.UploadedSkill{{Name: "go", Path: "/skills/go/SKILL.md"}}, nil
	}
	var paths []string
	for _, file := range up.Files {
		rc, _ := file.Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		paths = append(paths, file.Path+"="+string(data))
	}
	f.record("upload folder %s %v for %q by %s", up.Name, paths, up.Team, up.UserID)
	return []hub.UploadedSkill{{Name: "go", Code: "skillExists", Params: store.Params{"name": "go"}, Message: "the skill library has a skill go already"}}, nil
}

func (f *fakeWikis) ExportSkill(_ context.Context, name string) ([]byte, error) {
	f.record("export %s", name)
	if name != "go" {
		return nil, store.ErrNotFound
	}
	return []byte("PK zip"), nil
}

func (f *fakeWikis) InstallSkill(ctx context.Context, name, agentID string, installed bool) ([]store.AgentRef, error) {
	f.record("install %s for %s: %v", name, agentID, installed)
	if installed {
		if err := f.CheckSkills(ctx, []string{name}); err != nil {
			return nil, err
		}
		return []store.AgentRef{{ID: agentID, Name: "Coder"}}, nil
	}
	return []store.AgentRef{}, nil
}

func (f *fakeWikis) RollbackSkill(ctx context.Context, name, userID, reason string) (hub.WikiPageView, error) {
	f.record("rollback %s by %s: %s", name, userID, reason)
	if name == "steady" {
		return hub.WikiPageView{}, store.Missing("noTrial", store.Params{"name": name}, "the skill %s is on no trial", name)
	}
	return f.LibraryPage(ctx, "/skills/"+name+"/SKILL.md")
}

func (f *fakeWikis) CheckSkills(_ context.Context, names []string) error {
	for _, name := range names {
		if name == "nope" || slices.Contains(f.retired, name) {
			return store.Missing("skillUnknown", store.Params{"name": name}, "the skill library has no skill %s", name)
		}
	}
	return nil
}

func (f *fakeWikis) UpkeepStatus(_ context.Context, projectID string) (hub.UpkeepStatus, error) {
	if err := f.project(projectID); err != nil {
		return hub.UpkeepStatus{}, err
	}
	f.record("upkeep status %s", projectID)
	return hub.UpkeepStatus{MemberID: "m1", MemberName: "Keeper", Trigger: store.UpkeepIdle, Waiting: store.UpkeepWaiting{Own: 3}}, nil
}

func (f *fakeWikis) StartUpkeep(_ context.Context, projectID string) (hub.UpkeepStatus, error) {
	switch projectID {
	case "busy":
		return hub.UpkeepStatus{}, fmt.Errorf("%w: running", store.ErrConflict)
	case "nobody":
		return hub.UpkeepStatus{}, fmt.Errorf("%w: %w", store.ErrInvalidInput, hub.ErrNoMaintainer)
	}
	f.record("upkeep %s", projectID)
	return hub.UpkeepStatus{MemberID: "m1", Queued: true}, nil
}

func wikiHandler() (http.Handler, *fakeWikis) {
	fake := &fakeWikis{pages: map[string]hub.WikiPageView{
		"/facts/a.md": {WikiPageInfo: hub.WikiPageInfo{Path: "/facts/a.md", Type: "Fact", Title: "A", Tags: []string{}}, Body: "A."},
	}}
	return NewHandler(Deps{Wikis: fake}), fake
}

func TestWikiReads(t *testing.T) {
	handler, fake := wikiHandler()
	var catalog WikiCatalogResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/projects/p1/wiki", "", &catalog); rec.Code != http.StatusOK || len(catalog.Wiki.Pages) != 1 || !catalog.Wiki.History {
		t.Errorf("catalog: %d %+v", rec.Code, catalog)
	}
	var page WikiPageResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/projects/p1/wiki/page?path=%2Ffacts%2Fa.md", "", &page); rec.Code != http.StatusOK || page.Page.Body != "A." {
		t.Errorf("page: %d %+v", rec.Code, page)
	}
	var hits WikiSearchResponse
	do(t, handler, http.MethodGet, "/api/v1/projects/p1/wiki/search?q=a+b&limit=500", "", &hits)
	var history WikiHistoryResponse
	do(t, handler, http.MethodGet, "/api/v1/projects/p1/wiki/history?path=/facts/a.md&limit=5", "", &history)
	if len(hits.Hits) != 1 || len(history.Commits) != 1 || !history.Commits[0].Undoable {
		t.Errorf("hits %+v, history %+v", hits, history)
	}
	want := []string{`search p1 "a b" 200`, `history p1 "/facts/a.md" 5`}
	if fmt.Sprint(fake.asked) != fmt.Sprint(want) {
		t.Errorf("asked %q, want %q", fake.asked, want)
	}

	for path, status := range map[string]int{
		"/api/v1/projects/p404/wiki":                  http.StatusNotFound,
		"/api/v1/projects/p1/wiki/page":               http.StatusBadRequest,
		"/api/v1/projects/p1/wiki/page?path=/nope.md": http.StatusNotFound,
		"/api/v1/projects/p1/wiki/search?limit=-1":    http.StatusBadRequest,
	} {
		if rec := do(t, handler, http.MethodGet, path, "", nil); rec.Code != status {
			t.Errorf("GET %s = %d, want %d: %s", path, rec.Code, status, rec.Body)
		}
	}
}

func TestWikiChangesByAPerson(t *testing.T) {
	handler, fake := wikiHandler()
	var reverted RevertWikiResponse
	if rec := do(t, handler, http.MethodPost, "/api/v1/projects/p1/wiki/revert", `{"user_id":"u1","sha":"abc","reason":"not yet"}`, &reverted); rec.Code != http.StatusOK || reverted.Commit != "def" {
		t.Errorf("revert: %d %+v", rec.Code, reverted)
	}
	if !slices.Contains(fake.asked, "revert p1 abc by u1: not yet") {
		t.Errorf("the reason reaches the hub: %v", fake.asked)
	}
	var page WikiPageResponse
	do(t, handler, http.MethodPost, "/api/v1/projects/p1/wiki/verify", `{"user_id":"u1","path":"/facts/a.md"}`, &page)
	do(t, handler, http.MethodPost, "/api/v1/projects/p1/wiki/resident", `{"user_id":"u1","path":"/facts/a.md","resident":true}`, &page)
	want := []string{"revert p1 abc by u1: not yet", "verify p1 /facts/a.md by u1", "resident p1 /facts/a.md true by u1"}
	if fmt.Sprint(fake.asked) != fmt.Sprint(want) {
		t.Errorf("asked %q, want %q", fake.asked, want)
	}

	for _, tc := range []struct {
		path, body string
		status     int
	}{
		{"/api/v1/projects/p1/wiki/revert", `{"sha":"abc"}`, http.StatusBadRequest},
		{"/api/v1/projects/p1/wiki/revert", `{"user_id":"u1"}`, http.StatusBadRequest},
		{"/api/v1/projects/p1/wiki/revert", `{"user_id":"u1","sha":"tangled"}`, http.StatusConflict},
		{"/api/v1/projects/p1/wiki/verify", `{"user_id":"u1"}`, http.StatusBadRequest},
		{"/api/v1/projects/p1/wiki/resident", `{"user_id":"u1","path":"/nope.md"}`, http.StatusNotFound},
	} {
		if rec := do(t, handler, http.MethodPost, tc.path, tc.body, nil); rec.Code != tc.status {
			t.Errorf("POST %s %s = %d, want %d: %s", tc.path, tc.body, rec.Code, tc.status, rec.Body)
		}
	}
}

func TestWikiQuestion(t *testing.T) {
	handler, fake := wikiHandler()
	var asked WikiQuestionResponse
	if rec := do(t, handler, http.MethodPost, "/api/v1/projects/p1/wiki/question", `{}`, &asked); rec.Code != http.StatusOK || asked.Question.ThreadID != "t9" || asked.Question.MemberName != "Keeper" {
		t.Errorf("question: %d %+v", rec.Code, asked)
	}
	if !slices.Contains(fake.asked, "question p1") {
		t.Errorf("asked %v", fake.asked)
	}
	rec := do(t, handler, http.MethodPost, "/api/v1/projects/empty/wiki/question", `{}`, nil)
	var refused ErrorResponse
	if json.Unmarshal(rec.Body.Bytes(), &refused); rec.Code != http.StatusConflict || refused.Code != "noOneToAsk" {
		t.Errorf("nobody to ask: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/projects/p404/wiki/question", `{}`, nil); rec.Code != http.StatusNotFound {
		t.Errorf("no such project: %d", rec.Code)
	}
}

func TestWikiWithoutWikis(t *testing.T) {
	handler := NewHandler(Deps{})
	for _, path := range []string{"/api/v1/projects/p1/wiki", "/api/v1/library"} {
		if rec := do(t, handler, http.MethodGet, path, "", nil); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s without wikis = %d, want 404", path, rec.Code)
		}
	}
}

func TestUpdateProject_WikiSettings(t *testing.T) {
	handler, fake := projectsHandler()
	var created ProjectResponse
	do(t, handler, http.MethodPost, "/api/v1/projects", `{"name":"veyloom"}`, &created)
	do(t, handler, http.MethodPatch, "/api/v1/projects/"+created.Project.ID, `{"name":"x"}`, nil)
	if fake.patched.WikiMaintainer != nil || fake.patched.WikiMaintainerTrigger != nil || fake.patched.WikiExternalBundles != nil || fake.patched.WikiUpkeep != nil || fake.patched.Leader != nil {
		t.Errorf("left alone when absent: %+v", fake.patched)
	}
	do(t, handler, http.MethodPatch, "/api/v1/projects/"+created.Project.ID, `{"wiki_upkeep":true,"leader_member_id":"m2"}`, nil)
	if u, l := fake.patched.WikiUpkeep, fake.patched.Leader; u == nil || !*u || l == nil || *l != "m2" || fake.patched.WikiMaintainer != nil {
		t.Errorf("upkeep on and the leader %+v", fake.patched)
	}
	do(t, handler, http.MethodPatch, "/api/v1/projects/"+created.Project.ID, `{"wiki_maintainer_member_id":"m1","wiki_maintainer_trigger":"daily"}`, nil)
	if m, tr := fake.patched.WikiMaintainer, fake.patched.WikiMaintainerTrigger; m == nil || *m != "m1" || tr == nil || *tr != store.UpkeepDaily {
		t.Errorf("the maintainer %+v", fake.patched)
	}
	do(t, handler, http.MethodPatch, "/api/v1/projects/"+created.Project.ID, `{"wiki_maintainer_member_id":""}`, nil)
	if m := fake.patched.WikiMaintainer; m == nil || *m != "" || fake.patched.DeclineWikiOffer {
		t.Errorf("no maintainer %+v", fake.patched)
	}
	do(t, handler, http.MethodPatch, "/api/v1/projects/"+created.Project.ID, `{"wiki_offer_declined":true}`, nil)
	if !fake.patched.DeclineWikiOffer || fake.patched.WikiMaintainer != nil {
		t.Errorf("a person said no to the offer: %+v", fake.patched)
	}

	// A maintainer chosen as the project is created goes to the store as is.
	do(t, handler, http.MethodPost, "/api/v1/projects", `{"name":"kept","agent_ids":["g1"],"wiki_upkeep":true,"wiki_maintainer_agent_id":" g1 ","wiki_maintainer_trigger":"weekly"}`, nil)
	if c := fake.created; !c.WikiUpkeep || c.WikiMaintainerAgentID != "g1" || c.WikiMaintainerTrigger != store.UpkeepWeekly {
		t.Errorf("created with %+v", c)
	}

	// Mounted bundles are folders on this machine, checked before they are
	// stored.
	dir := t.TempDir()
	body, _ := json.Marshal(map[string]any{"wiki_external_bundles": []string{dir, " ", dir}})
	do(t, handler, http.MethodPatch, "/api/v1/projects/"+created.Project.ID, string(body), nil)
	if b := fake.patched.WikiExternalBundles; b == nil || len(*b) != 1 || (*b)[0] != dir {
		t.Errorf("mounted %+v", fake.patched.WikiExternalBundles)
	}
	// Refused in words a person reads, without the store's sentinel.
	rec := do(t, handler, http.MethodPatch, "/api/v1/projects/"+created.Project.ID, `{"wiki_external_bundles":["relative"]}`, nil)
	var refused ErrorResponse
	if json.Unmarshal(rec.Body.Bytes(), &refused); rec.Code != http.StatusBadRequest || !strings.HasPrefix(refused.Error, "relative is not a full path") ||
		refused.Code != "mountNotAbsolute" || refused.Params["path"] != "relative" {
		t.Errorf("a relative folder: %d %q", rec.Code, refused.Error)
	}
}

func TestLibrary(t *testing.T) {
	handler, fake := wikiHandler()
	var catalog WikiCatalogResponse
	if do(t, handler, http.MethodGet, "/api/v1/library", "", &catalog); len(catalog.Wiki.Teams) != 1 || catalog.Wiki.Pages[0].Team != "veyloom" {
		t.Errorf("catalog %+v", catalog)
	}
	var page WikiPageResponse
	if do(t, handler, http.MethodGet, "/api/v1/library/page?path=/skills/go/SKILL.md", "", &page); page.Page.Body != "Use tables." {
		t.Errorf("page %+v", page)
	}
	var uses SkillUsesResponse
	if do(t, handler, http.MethodGet, "/api/v1/library/usage?name=go&limit=5", "", &uses); len(uses.Uses) != 1 || uses.Uses[0].TopicNumber != 3 {
		t.Errorf("uses %+v", uses)
	}
	do(t, handler, http.MethodGet, "/api/v1/library/search?q=table", "", nil)
	do(t, handler, http.MethodGet, "/api/v1/library/history?path=/skills/go/SKILL.md", "", nil)
	do(t, handler, http.MethodPost, "/api/v1/library/revert", `{"user_id":"u1","sha":"abc"}`, nil)
	do(t, handler, http.MethodPost, "/api/v1/library/verify", `{"user_id":"u1","path":"/skills/go/SKILL.md"}`, nil)
	do(t, handler, http.MethodPost, "/api/v1/library/transfer", `{"user_id":"u1","name":"go","project_id":"p2"}`, &page)
	if rec := do(t, handler, http.MethodPost, "/api/v1/library/import", `{"user_id":"u1","folder":"/skills/go","project_id":"p2"}`, &page); rec.Code != http.StatusCreated || page.Page.Path != "/skills/go/SKILL.md" {
		t.Errorf("import: %d %+v", rec.Code, page)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/library/update", `{"user_id":"u1","folder":"/home/me/.claude/skills/go"}`, &page); rec.Code != http.StatusOK || page.Page.Path != "/skills/go/SKILL.md" {
		t.Errorf("update: %d %+v", rec.Code, page)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/library/retire", `{"user_id":"u1","name":"go","retired":true}`, &page); rec.Code != http.StatusOK || page.Page.Path != "/skills/go/SKILL.md" {
		t.Errorf("retire: %d %+v", rec.Code, page)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/library/delete", `{"user_id":"u1","name":"go"}`, nil); rec.Code != http.StatusNoContent {
		t.Errorf("delete: %d", rec.Code)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/library/rollback", `{"user_id":"u1","name":"go","reason":"longer notes"}`, &page); rec.Code != http.StatusOK || page.Page.Path != "/skills/go/SKILL.md" {
		t.Errorf("rollback: %d %+v", rec.Code, page)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/library/export?name=go", "", nil); rec.Code != http.StatusOK ||
		rec.Header().Get("Content-Type") != "application/zip" || rec.Header().Get("Content-Disposition") != `attachment; filename=go.zip` || rec.Body.String() != "PK zip" {
		t.Errorf("export: %d %v %q", rec.Code, rec.Header(), rec.Body)
	}
	var installs SkillInstallsResponse
	if do(t, handler, http.MethodPost, "/api/v1/library/install", `{"name":"go","agent_id":"ag1","installed":true}`, &installs); len(installs.Installed) != 1 || installs.Installed[0].Name != "Coder" {
		t.Errorf("installed %+v", installs)
	}
	if do(t, handler, http.MethodPost, "/api/v1/library/install", `{"name":"go","agent_id":"ag1"}`, &installs); installs.Installed == nil || len(installs.Installed) != 0 {
		t.Errorf("taken off %+v", installs)
	}
	want := []string{`uses go 5`, `library search "table" 200`, `library history "/skills/go/SKILL.md" 200`, "library revert abc by u1",
		"library verify /skills/go/SKILL.md by u1", "transfer go to p2 by u1", `import /skills/go for "p2" by u1`,
		"update from /home/me/.claude/skills/go by u1", "retire go true by u1", "delete go by u1", "rollback go by u1: longer notes", "export go", "install go for ag1: true", "install go for ag1: false"}
	if fmt.Sprint(fake.asked) != fmt.Sprint(want) {
		t.Errorf("asked %q, want %q", fake.asked, want)
	}
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{http.MethodGet, "/api/v1/library/page", "", http.StatusBadRequest},
		{http.MethodGet, "/api/v1/library/page?path=/nope.md", "", http.StatusNotFound},
		{http.MethodGet, "/api/v1/library/usage", "", http.StatusBadRequest},
		{http.MethodPost, "/api/v1/library/transfer", `{"user_id":"u1","name":"go"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/library/transfer", `{"user_id":"u1","name":"go","project_id":"p404"}`, http.StatusNotFound},
		{http.MethodPost, "/api/v1/library/revert", `{"user_id":"u1"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/library/import", `{"user_id":"u1"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/library/import", `{"user_id":"u1","folder":"relative"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/library/update", `{"user_id":"u1"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/library/update", `{"user_id":"u1","folder":"/on/trial"}`, http.StatusConflict},
		{http.MethodPost, "/api/v1/library/retire", `{"user_id":"u1"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/library/delete", `{"user_id":"u1"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/library/delete", `{"user_id":"u1","name":"busy"}`, http.StatusConflict},
		{http.MethodPost, "/api/v1/library/install", `{"name":"go"}`, http.StatusBadRequest},
		{http.MethodGet, "/api/v1/library/export", "", http.StatusBadRequest},
		{http.MethodGet, "/api/v1/library/export?name=nope", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/library/rollback", `{"user_id":"u1"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/library/rollback", `{"user_id":"u1","name":"steady"}`, http.StatusNotFound},
		{http.MethodPost, "/api/v1/library/install", `{"name":"nope","agent_id":"ag1","installed":true}`, http.StatusNotFound},
	} {
		if rec := do(t, handler, tc.method, tc.path, tc.body, nil); rec.Code != tc.status {
			t.Errorf("%s %s %s = %d, want %d: %s", tc.method, tc.path, tc.body, rec.Code, tc.status, rec.Body)
		}
	}
}

// Skills from this machine's folders, and from the browser: a zip, or a
// folder's files with their paths beside them.
func TestLibraryLocalAndUpload(t *testing.T) {
	handler, fake := wikiHandler()
	var local LocalSkillsResponse
	if do(t, handler, http.MethodGet, "/api/v1/library/local", "", &local); len(local.Skills) != 1 || local.Skills[0].Where != "~/.agents/skills" {
		t.Errorf("local %+v", local)
	}

	send := func(fields [][2]string, files [][2]string) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		for _, f := range fields {
			mw.WriteField(f[0], f[1])
		}
		for _, f := range files {
			part, _ := mw.CreateFormFile("file", f[0])
			part.Write([]byte(f[1]))
		}
		mw.Close()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/library/upload", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	var uploaded UploadSkillsResponse
	rec := send([][2]string{{"user_id", "u1"}, {"project_id", "p2"}}, [][2]string{{"go.zip", "PK zip"}})
	if json.Unmarshal(rec.Body.Bytes(), &uploaded); rec.Code != http.StatusOK || len(uploaded.Skills) != 1 || uploaded.Skills[0].Path != "/skills/go/SKILL.md" {
		t.Errorf("zip: %d %s", rec.Code, rec.Body)
	}
	// A folder: the paths go in fields of their own, a file name keeping
	// no folders.
	rec = send([][2]string{{"user_id", "u1"}, {"path", "go/SKILL.md"}, {"path", "go/scripts/run.sh"}}, [][2]string{{"SKILL.md", "skill"}, {"run.sh", "script"}})
	if json.Unmarshal(rec.Body.Bytes(), &uploaded); rec.Code != http.StatusOK || uploaded.Skills[0].Code != "skillExists" {
		t.Errorf("folder: %d %s", rec.Code, rec.Body)
	}
	want := []string{"local skills", `upload zip go.zip (6 bytes) for "p2" by u1`, `upload folder go [go/SKILL.md=skill go/scripts/run.sh=script] for "" by u1`}
	if fmt.Sprint(fake.asked) != fmt.Sprint(want) {
		t.Errorf("asked %q, want %q", fake.asked, want)
	}
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"no files":                send([][2]string{{"user_id", "u1"}}, nil),
		"paths that do not match": send([][2]string{{"user_id", "u1"}, {"path", "a/SKILL.md"}}, [][2]string{{"SKILL.md", "x"}, {"b.md", "y"}}),
		"no person":               send(nil, [][2]string{{"go.zip", "PK"}}),
	} {
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
}

func TestUpkeep(t *testing.T) {
	handler, fake := wikiHandler()
	var status UpkeepResponse
	if do(t, handler, http.MethodGet, "/api/v1/projects/p1/wiki/maintainer", "", &status); status.Upkeep.MemberName != "Keeper" || status.Upkeep.Waiting.Own != 3 {
		t.Errorf("status %+v", status)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/projects/p1/wiki/maintain", nil))
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"queued":true`) {
		t.Errorf("start: %d %s", rec.Code, rec.Body)
	}
	for path, want := range map[string]int{
		"/api/v1/projects/busy/wiki/maintain":   http.StatusConflict,
		"/api/v1/projects/nobody/wiki/maintain": http.StatusBadRequest,
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		if rec.Code != want {
			t.Errorf("%s: %d, want %d", path, rec.Code, want)
		}
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/projects/p404/wiki/maintainer", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("an unknown project: %d", rec.Code)
	}
	if want := []string{"upkeep status p1", "upkeep p1"}; fmt.Sprint(fake.asked) != fmt.Sprint(want) {
		t.Errorf("asked %q, want %q", fake.asked, want)
	}
}
