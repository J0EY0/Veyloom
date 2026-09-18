package api

import (
	"bytes"
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

var (
	pngImage  = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 64)...)
	webpImage = append([]byte("RIFF\x24\x00\x00\x00WEBPVP8 "), bytes.Repeat([]byte{9}, 64)...)
)

// avatarHandler wires the agent fakes, a known machine "w1" and an avatar
// directory of its own.
func avatarHandler(t *testing.T) (http.Handler, *fakeAgents, string) {
	t.Helper()
	projects := newFakeProjects()
	agents := newFakeAgents(projects, "w1")
	dir := t.TempDir()
	return NewHandler(Deps{Projects: projects, Agents: agents, AvatarDir: dir}), agents, dir
}

func uploadAvatar(t *testing.T, handler http.Handler, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/avatars", bytes.NewReader(data)))
	return rec
}

func uploadedAvatar(t *testing.T, handler http.Handler, data []byte) string {
	t.Helper()
	rec := uploadAvatar(t, handler, data)
	var got AvatarResponse
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("upload: status = %d, body %s", rec.Code, rec.Body)
	}
	return got.Avatar
}

func gone(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	return errors.Is(err, os.ErrNotExist)
}

func TestAvatars_UploadAndServe(t *testing.T) {
	handler, _, dir := avatarHandler(t)

	name := uploadedAvatar(t, handler, pngImage)
	if !avatarName.MatchString(name) || !strings.HasSuffix(name, ".png") {
		t.Fatalf("name = %q, want a hash and .png", name)
	}
	// The same picture again is the same file.
	if again := uploadedAvatar(t, handler, pngImage); again != name {
		t.Errorf("same bytes got another name: %q, %q", name, again)
	}
	if files, err := os.ReadDir(dir); err != nil || len(files) != 1 {
		t.Errorf("want one file in the avatar directory, got %v, %v", files, err)
	}

	// It is served as the image it is, and never changes under its name.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+name, nil))
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), pngImage) {
		t.Fatalf("get: status = %d, %d bytes", rec.Code, rec.Body.Len())
	}
	if rec.Header().Get("Content-Type") != "image/png" || rec.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("unexpected headers: %v", rec.Header())
	}
	if webp := uploadedAvatar(t, handler, webpImage); !strings.HasSuffix(webp, ".webp") {
		t.Errorf("webp name = %q", webp)
	}
}

func TestAvatars_Refused(t *testing.T) {
	handler, _, _ := avatarHandler(t)

	// The first bytes decide: an SVG can carry scripts and is no avatar.
	if rec := uploadAvatar(t, handler, []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("svg: status = %d, want 415", rec.Code)
	}
	if rec := uploadAvatar(t, handler, append(append([]byte{}, pngImage...), make([]byte, MaxAvatarSize)...)); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("too large: status = %d, want 413", rec.Code)
	}
	// Only names an upload can have are looked up.
	for _, name := range []string{"0123456789abcdef0123456789abcdef.png", "passwd", "0123456789abcdef0123456789abcdef.svg"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+name, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("get %s: status = %d, want 404", name, rec.Code)
		}
	}
	// Without a directory the hub takes none.
	off := NewHandler(Deps{Projects: newFakeProjects(), Agents: newFakeAgents(newFakeProjects())})
	if rec := uploadAvatar(t, off, pngImage); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("unconfigured: status = %d, want 503", rec.Code)
	}
}

func TestAgents_Avatar(t *testing.T) {
	handler, agents, dir := avatarHandler(t)
	body := func(avatar string) string {
		return fmt.Sprintf(`{"name":"Reviewer","avatar":%q,"machine_id":"w1","runtime":"claude","permission_preset":"read_only"}`, avatar)
	}

	// Only a picture that was uploaded can be named.
	if rec := do(t, handler, http.MethodPost, "/api/v1/agents", body("0123456789abcdef0123456789abcdef.png"), nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown avatar: status = %d, want 400", rec.Code)
	}
	first := uploadedAvatar(t, handler, pngImage)
	var created AgentResponse
	if rec := do(t, handler, http.MethodPost, "/api/v1/agents", body(first), &created); rec.Code != http.StatusCreated || created.Agent.Avatar != first {
		t.Fatalf("create: status = %d, agent %+v", rec.Code, created.Agent)
	}

	// Another picture replaces it, and the first file goes with it.
	second := uploadedAvatar(t, handler, webpImage)
	var updated AgentResponse
	path := "/api/v1/agents/" + created.Agent.ID
	if rec := do(t, handler, http.MethodPut, path, body(second), &updated); rec.Code != http.StatusOK || updated.Agent.Avatar != second {
		t.Fatalf("replace: status = %d, agent %+v", rec.Code, updated.Agent)
	}
	if !gone(t, filepath.Join(dir, first)) {
		t.Errorf("the replaced avatar should be removed")
	}

	// A picture another agent still shows stays when this one lets it go.
	twin, err := agents.CreateAgent(context.Background(), store.NewAgent{Name: "Twin", Avatar: second, MachineID: "w1", Runtime: "pi", PermissionPreset: store.PermissionReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	if rec := do(t, handler, http.MethodPut, path, body(""), &updated); rec.Code != http.StatusOK || updated.Agent.Avatar != "" {
		t.Fatalf("clear: status = %d, agent %+v", rec.Code, updated.Agent)
	}
	if gone(t, filepath.Join(dir, second)) {
		t.Errorf("an avatar the twin shows should stay")
	}
	// Deleting the last agent that shows it removes the file.
	if rec := do(t, handler, http.MethodDelete, "/api/v1/agents/"+twin.ID, "", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d, body %s", rec.Code, rec.Body)
	}
	if !gone(t, filepath.Join(dir, second)) {
		t.Errorf("the avatar of a deleted agent should be removed")
	}
}
