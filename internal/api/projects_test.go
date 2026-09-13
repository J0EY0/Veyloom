package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// fakeProjects is an in-memory ProjectStore. IDs are "p1", "r1", ...; the
// id "bad" is treated as malformed so tests can exercise the 400 path.
type fakeProjects struct {
	projects map[string]store.Project
	rooms    map[string]store.Room
}

func newFakeProjects() *fakeProjects {
	return &fakeProjects{projects: map[string]store.Project{}, rooms: map[string]store.Room{}}
}

func checkID(id string) error {
	if id == "bad" {
		return fmt.Errorf("%w: %q", store.ErrInvalidID, id)
	}
	return nil
}

func (f *fakeProjects) CreateProject(_ context.Context, p store.NewProject) (store.Project, store.Room, error) {
	project := store.Project{ID: fmt.Sprintf("p%d", len(f.projects)+1), Name: p.Name, RepoURL: p.RepoURL, DefaultBranch: p.DefaultBranch}
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
	handler, _ := projectsHandler()

	var resp ProjectResponse
	rec := do(t, handler, http.MethodPost, "/api/v1/projects", `{"name":" veyloom ","repo_url":"git@x:y.git"}`, &resp)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body)
	}
	if resp.Project.Name != "veyloom" {
		t.Errorf("name should be trimmed, got %q", resp.Project.Name)
	}
	if len(resp.Rooms) != 1 || resp.Rooms[0].Kind != store.RoomMain {
		t.Errorf("response should include the main room, got %+v", resp.Rooms)
	}
}

func TestCreateProject_BadRequests(t *testing.T) {
	handler, _ := projectsHandler()

	for name, body := range map[string]string{
		"blank name":     `{"name":"   "}`,
		"missing name":   `{"repo_url":"x"}`,
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
