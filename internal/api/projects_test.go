package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// fakeProjects is an in-memory ProjectStore. IDs are "p1", "r1", ...; the
// id "bad" is treated as malformed so tests can exercise the 400 path.
type fakeProjects struct {
	projects map[string]store.Project
	rooms    map[string]store.Room
	// created is the last project asked for.
	created store.NewProject
	// patched is the last change asked for.
	patched store.ProjectPatch
	// busy projects have a turn running; remains is what a delete leaves.
	busy    map[string]bool
	remains store.ProjectRemains
}

func newFakeProjects() *fakeProjects {
	return &fakeProjects{projects: map[string]store.Project{}, rooms: map[string]store.Room{}, busy: map[string]bool{}}
}

func checkID(id string) error {
	if id == "bad" {
		return fmt.Errorf("%w: %q", store.ErrInvalidID, id)
	}
	return nil
}

func (f *fakeProjects) CreateProject(_ context.Context, p store.NewProject) (store.Project, store.Room, error) {
	f.created = p
	project := store.Project{ID: fmt.Sprintf("p%d", len(f.projects)+1), Name: p.Name, RepoPath: p.RepoPath, Description: p.Description}
	f.projects[project.ID] = project
	room := store.Room{ID: fmt.Sprintf("r%d", len(f.rooms)+1), ProjectID: project.ID, Name: store.MainRoomName, Kind: store.RoomMain}
	f.rooms[room.ID] = room
	return project, room, nil
}

func (f *fakeProjects) GetProject(_ context.Context, id string) (store.Project, error) {
	if err := checkID(id); err != nil {
		return store.Project{}, err
	}
	p, ok := f.projects[id]
	if !ok {
		return store.Project{}, fmt.Errorf("project %s: %w", id, store.ErrNotFound)
	}
	return p, nil
}

func (f *fakeProjects) ListProjects(context.Context) ([]store.Project, error) {
	out := []store.Project{}
	for _, p := range f.projects {
		out = append(out, p)
	}
	return out, nil
}

func (f *fakeProjects) UpdateProject(_ context.Context, id string, patch store.ProjectPatch) (store.Project, error) {
	p, err := f.GetProject(context.Background(), id)
	if err != nil {
		return store.Project{}, err
	}
	f.patched = patch
	if patch.Name != nil {
		p.Name = *patch.Name
	}
	if patch.RepoPath != nil {
		p.RepoPath = *patch.RepoPath
	}
	if patch.Description != nil {
		p.Description = *patch.Description
	}
	f.projects[id] = p
	return p, nil
}

func (f *fakeProjects) DeleteProject(_ context.Context, id string) (store.ProjectRemains, error) {
	if _, err := f.GetProject(context.Background(), id); err != nil {
		return store.ProjectRemains{}, err
	}
	if f.busy[id] {
		return store.ProjectRemains{}, fmt.Errorf("project %s: %w: a turn is still running", id, store.ErrConflict)
	}
	delete(f.projects, id)
	for roomID, room := range f.rooms {
		if room.ProjectID == id {
			delete(f.rooms, roomID)
		}
	}
	return f.remains, nil
}

func (f *fakeProjects) CreateRoom(_ context.Context, projectID, name string) (store.Room, error) {
	if _, err := f.GetProject(context.Background(), projectID); err != nil {
		return store.Room{}, err
	}
	room := store.Room{ID: fmt.Sprintf("r%d", len(f.rooms)+1), ProjectID: projectID, Name: name, Kind: store.RoomTopic}
	f.rooms[room.ID] = room
	return room, nil
}

func (f *fakeProjects) GetRoom(_ context.Context, id string) (store.Room, error) {
	if err := checkID(id); err != nil {
		return store.Room{}, err
	}
	r, ok := f.rooms[id]
	if !ok {
		return store.Room{}, fmt.Errorf("room %s: %w", id, store.ErrNotFound)
	}
	return r, nil
}

func (f *fakeProjects) ListRooms(_ context.Context, projectID string) ([]store.Room, error) {
	out := []store.Room{}
	for _, r := range f.rooms {
		if r.ProjectID == projectID {
			out = append(out, r)
		}
	}
	return out, nil
}

// do performs a request against a fresh handler and decodes the JSON body
// into out when out is not nil.
func do(t *testing.T, handler http.Handler, method, path, body string, out any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(method, path, reader))
	if out != nil && rec.Code < 300 {
		if err := json.NewDecoder(rec.Body).Decode(out); err != nil {
			t.Fatalf("%s %s: decode body: %v\n%s", method, path, err, rec.Body)
		}
	}
	return rec
}

func projectsHandler() (http.Handler, *fakeProjects) {
	fake := newFakeProjects()
	return NewHandler(Deps{Projects: fake}), fake
}

func TestCreateProject(t *testing.T) {
	handler, fake := projectsHandler()

	var resp ProjectResponse
	rec := do(t, handler, http.MethodPost, "/api/v1/projects", `{"name":" veyloom ","repo_path":" /src/veyloom ","agent_ids":["ag1"," ag2 "]}`, &resp)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body)
	}
	if resp.Project.Name != "veyloom" {
		t.Errorf("name should be trimmed, got %q", resp.Project.Name)
	}
	if len(resp.Rooms) != 1 || resp.Rooms[0].Kind != store.RoomMain {
		t.Errorf("response should include the main room, got %+v", resp.Rooms)
	}
	// The path and the agents to add go to the store, trimmed.
	if fake.created.RepoPath != "/src/veyloom" || strings.Join(fake.created.AgentIDs, ",") != "ag1,ag2" || resp.Project.RepoPath != "/src/veyloom" {
		t.Errorf("unexpected project asked for: %+v, answered %+v", fake.created, resp.Project)
	}
}

func TestCreateProject_TooLarge(t *testing.T) {
	handler, _ := projectsHandler()
	body := `{"name":"x","description":"` + strings.Repeat("长", maxBodyBytes/3+1) + `"}`
	rec := do(t, handler, http.MethodPost, "/api/v1/projects", body, nil)
	var refused ErrorResponse
	if json.Unmarshal(rec.Body.Bytes(), &refused); rec.Code != http.StatusBadRequest || refused.Code != "requestTooLarge" || refused.Params["mb"] != "1" {
		t.Errorf("a body over the limit: %d %s", rec.Code, rec.Body)
	}
	// Within it, a description is counted in characters, as the web client
	// counts it: 4000 Chinese ones are 12000 bytes.
	var created ProjectResponse
	if rec := do(t, handler, http.MethodPost, "/api/v1/projects", `{"name":"x","description":"`+strings.Repeat("长", maxProjectDescription)+`"}`, &created); rec.Code != http.StatusCreated {
		t.Errorf("a description of %d characters: %d %s", maxProjectDescription, rec.Code, rec.Body)
	}
	rec = do(t, handler, http.MethodPost, "/api/v1/projects", `{"name":"x","description":"`+strings.Repeat("长", maxProjectDescription+1)+`"}`, nil)
	if json.Unmarshal(rec.Body.Bytes(), &refused); rec.Code != http.StatusBadRequest || refused.Code != "descriptionTooLong" || refused.Params["max"] != "4000" {
		t.Errorf("a description over it: %d %s", rec.Code, rec.Body)
	}
}

func TestCreateProject_BadRequests(t *testing.T) {
	handler, _ := projectsHandler()

	for name, body := range map[string]string{
		"blank name":     `{"name":"   "}`,
		"missing name":   `{"repo_path":"x"}`,
		"blank agent id": `{"name":"x","agent_ids":[" "]}`,
		"malformed json": `{"name":`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := do(t, handler, http.MethodPost, "/api/v1/projects", body, nil)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body: %s", rec.Code, rec.Body)
			}
			var errResp ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil || errResp.Error == "" {
				t.Errorf("400 must carry a JSON error message, got %q", rec.Body)
			}
		})
	}
}

func TestGetProject_WithRooms(t *testing.T) {
	handler, fake := projectsHandler()
	project, _, _ := fake.CreateProject(context.Background(), store.NewProject{Name: "p"})
	fake.CreateRoom(context.Background(), project.ID, "topic")

	var resp ProjectResponse
	rec := do(t, handler, http.MethodGet, "/api/v1/projects/"+project.ID, "", &resp)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body)
	}
	if resp.Project.ID != project.ID || len(resp.Rooms) != 2 {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestGetProject_NotFoundAndInvalid(t *testing.T) {
	handler, _ := projectsHandler()

	if rec := do(t, handler, http.MethodGet, "/api/v1/projects/p404", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown project: status = %d, want 404", rec.Code)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/projects/bad", "", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed id: status = %d, want 400", rec.Code)
	}
}

func TestListProjects(t *testing.T) {
	handler, fake := projectsHandler()
	fake.CreateProject(context.Background(), store.NewProject{Name: "a"})
	fake.CreateProject(context.Background(), store.NewProject{Name: "b"})

	var resp ProjectsResponse
	rec := do(t, handler, http.MethodGet, "/api/v1/projects", "", &resp)

	if rec.Code != http.StatusOK || len(resp.Projects) != 2 {
		t.Errorf("status = %d, projects = %+v", rec.Code, resp.Projects)
	}
}

func TestCreateRoom(t *testing.T) {
	handler, fake := projectsHandler()
	project, _, _ := fake.CreateProject(context.Background(), store.NewProject{Name: "p"})

	var resp RoomResponse
	rec := do(t, handler, http.MethodPost, "/api/v1/projects/"+project.ID+"/rooms", `{"name":"auth"}`, &resp)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body)
	}
	if resp.Room.Name != "auth" || resp.Room.Kind != store.RoomTopic || resp.Room.ProjectID != project.ID {
		t.Errorf("unexpected room: %+v", resp.Room)
	}

	if rec := do(t, handler, http.MethodPost, "/api/v1/projects/p404/rooms", `{"name":"x"}`, nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown project: status = %d, want 404", rec.Code)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/projects/"+project.ID+"/rooms", `{"name":""}`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("blank name: status = %d, want 400", rec.Code)
	}
}

func TestListRooms_UnknownProjectIs404(t *testing.T) {
	handler, fake := projectsHandler()
	project, _, _ := fake.CreateProject(context.Background(), store.NewProject{Name: "p"})

	var resp RoomsResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/projects/"+project.ID+"/rooms", "", &resp); rec.Code != http.StatusOK || len(resp.Rooms) != 1 {
		t.Errorf("status = %d, rooms = %+v", rec.Code, resp.Rooms)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/projects/p404/rooms", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown project: status = %d, want 404", rec.Code)
	}
}

func TestGetRoom(t *testing.T) {
	handler, fake := projectsHandler()
	_, main, _ := fake.CreateProject(context.Background(), store.NewProject{Name: "p"})

	var resp RoomResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/rooms/"+main.ID, "", &resp); rec.Code != http.StatusOK || resp.Room.ID != main.ID {
		t.Errorf("status = %d, room = %+v", rec.Code, resp.Room)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/rooms/r404", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown room: status = %d, want 404", rec.Code)
	}
}

func TestUpdateProject(t *testing.T) {
	handler, fake := projectsHandler()
	var created ProjectResponse
	do(t, handler, http.MethodPost, "/api/v1/projects", `{"name":"veyloom","repo_path":"/src/veyloom"}`, &created)
	path := "/api/v1/projects/" + created.Project.ID

	// Both fields, trimmed; a trailing slash comes off the path.
	var resp ProjectResponse
	rec := do(t, handler, http.MethodPatch, path, `{"name":" platform ","repo_path":" /work/platform/ "}`, &resp)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	if resp.Project.Name != "platform" || resp.Project.RepoPath != "/work/platform" || len(resp.Rooms) != 1 {
		t.Errorf("answered %+v", resp)
	}

	// An absent field is left alone; an empty path is a path, cleared.
	do(t, handler, http.MethodPatch, path, `{"repo_path":""}`, &resp)
	if fake.patched.Name != nil || fake.patched.RepoPath == nil || *fake.patched.RepoPath != "" || resp.Project.Name != "platform" {
		t.Errorf("patched %+v, answered %+v", fake.patched, resp.Project)
	}
	do(t, handler, http.MethodPatch, path, `{"repo_path":"/"}`, &resp)
	if resp.Project.RepoPath != "/" {
		t.Errorf("the root keeps its slash: %q", resp.Project.RepoPath)
	}

	// How many turns agents may wake one another to (docs/design.md 5.22).
	do(t, handler, http.MethodPatch, path, `{"relay_limit":12}`, &resp)
	if fake.patched.RelayLimit == nil || *fake.patched.RelayLimit != 12 || fake.patched.RepoPath != nil {
		t.Errorf("the relay limit patched: %+v", fake.patched)
	}
}

func TestUpdateProject_BadRequests(t *testing.T) {
	handler, _ := projectsHandler()
	var created ProjectResponse
	do(t, handler, http.MethodPost, "/api/v1/projects", `{"name":"veyloom"}`, &created)

	for name, body := range map[string]string{
		"blank name":     `{"name":"  "}`,
		"malformed json": `{"name":`,
	} {
		t.Run(name, func(t *testing.T) {
			if rec := do(t, handler, http.MethodPatch, "/api/v1/projects/"+created.Project.ID, body, nil); rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body: %s", rec.Code, rec.Body)
			}
		})
	}
	if rec := do(t, handler, http.MethodPatch, "/api/v1/projects/p404", `{"name":"x"}`, nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown project: status = %d, want 404", rec.Code)
	}
}

func TestDeleteProject(t *testing.T) {
	fake := newFakeProjects()
	attachments, transcripts := t.TempDir(), t.TempDir()
	handler := NewHandler(Deps{Projects: fake, AttachmentDir: attachments, TranscriptDir: transcripts})
	var created ProjectResponse
	do(t, handler, http.MethodPost, "/api/v1/projects", `{"name":"veyloom"}`, &created)
	path := "/api/v1/projects/" + created.Project.ID

	// What the chat kept on disk, and a neighbour that must stay.
	write := func(name string) string {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return name
	}
	upload := write(filepath.Join(attachments, "room1", "a.png"))
	neighbour := write(filepath.Join(attachments, "room2", "b.png"))
	transcript := write(filepath.Join(transcripts, "turn1.jsonl"))
	fake.remains = store.ProjectRemains{AttachmentPaths: []string{"room1/a.png", "../outside.png"}, TurnIDs: []string{"turn1"}}
	outside := write(filepath.Join(filepath.Dir(attachments), "outside.png"))

	fake.busy[created.Project.ID] = true
	if rec := do(t, handler, http.MethodDelete, path, "", nil); rec.Code != http.StatusConflict {
		t.Fatalf("a running turn: status = %d, want 409; body: %s", rec.Code, rec.Body)
	}
	fake.busy[created.Project.ID] = false

	if rec := do(t, handler, http.MethodDelete, path, "", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body: %s", rec.Code, rec.Body)
	}
	for _, gone := range []string{upload, filepath.Dir(upload), transcript} {
		if _, err := os.Stat(gone); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s should be gone: %v", gone, err)
		}
	}
	for _, kept := range []string{neighbour, outside} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("%s should stay: %v", kept, err)
		}
	}
	if rec := do(t, handler, http.MethodGet, path, "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("after delete: status = %d, want 404", rec.Code)
	}
	if rec := do(t, handler, http.MethodDelete, path, "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("deleting again: status = %d, want 404", rec.Code)
	}
}

func TestProjectDescription(t *testing.T) {
	handler, fake := projectsHandler()

	// Given when the project is created, trimmed.
	var created ProjectResponse
	rec := do(t, handler, http.MethodPost, "/api/v1/projects", `{"name":"veyloom","description":"  A chat for coding agents.\nGo and Postgres.  "}`, &created)
	if rec.Code != http.StatusCreated || created.Project.Description != "A chat for coding agents.\nGo and Postgres." {
		t.Fatalf("status %d, project %+v", rec.Code, created.Project)
	}
	path := "/api/v1/projects/" + created.Project.ID

	// Rewritten later; a patch without it leaves it; an empty one clears it.
	var resp ProjectResponse
	do(t, handler, http.MethodPatch, path, `{"description":" Now with memory. "}`, &resp)
	if resp.Project.Description != "Now with memory." {
		t.Errorf("after the patch: %q", resp.Project.Description)
	}
	do(t, handler, http.MethodPatch, path, `{"name":"renamed"}`, &resp)
	if fake.patched.Description != nil || resp.Project.Description != "Now with memory." {
		t.Errorf("a rename touched the description: patched %+v, answered %q", fake.patched, resp.Project.Description)
	}
	do(t, handler, http.MethodPatch, path, `{"description":""}`, &resp)
	if resp.Project.Description != "" {
		t.Errorf("an empty description clears it, got %q", resp.Project.Description)
	}

	// It opens every brief, so it is a paragraph and not a document.
	long := strings.Repeat("x", maxProjectDescription+1)
	if rec := do(t, handler, http.MethodPatch, path, `{"description":"`+long+`"}`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("too long on patch: status %d, want 400", rec.Code)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/projects", `{"name":"big","description":"`+long+`"}`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("too long on create: status %d, want 400", rec.Code)
	}
}
