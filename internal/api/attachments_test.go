package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// fakeAttachments is an in-memory AttachmentStore.
type fakeAttachments struct {
	rooms *fakeProjects
	rows  map[string]store.Attachment
}

func (f *fakeAttachments) CreateAttachment(ctx context.Context, a store.NewAttachment) (store.Attachment, error) {
	if _, err := f.rooms.GetRoom(ctx, a.RoomID); err != nil {
		return store.Attachment{}, err
	}
	row := store.Attachment{ID: a.ID, RoomID: a.RoomID, Filename: a.Filename, MediaType: a.MediaType, Size: a.Size, Path: a.Path}
	f.rows[a.ID] = row
	return row, nil
}

func (f *fakeAttachments) GetAttachment(_ context.Context, id string) (store.Attachment, error) {
	row, ok := f.rows[id]
	if !ok {
		return store.Attachment{}, fmt.Errorf("attachment %s: %w", id, store.ErrNotFound)
	}
	return row, nil
}

func attachmentHandler(t *testing.T) (http.Handler, store.Room, store.User, *fakeChat, string) {
	t.Helper()
	handler, room, user, messages := chatHandler(t)
	_ = handler
	dir := t.TempDir()
	attachments := &fakeAttachments{rooms: messages.rooms, rows: map[string]store.Attachment{}}
	chat := &fakeChat{messages: messages, running: map[string]bool{}}
	h := NewHandler(Deps{Projects: messages.rooms, Users: messages.users, Messages: messages, Turns: fakeTurns{}, Chat: chat, Attachments: attachments, AttachmentDir: dir})
	return h, room, user, chat, dir
}

// upload builds a multipart request with one file field.
func upload(t *testing.T, handler http.Handler, path, filename, contentType string, content []byte, out any) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	part, err := mw.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if out != nil && rec.Code < 300 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("decode %s: %v", rec.Body, err)
		}
	}
	return rec
}

func TestAttachments_UploadServeAndPost(t *testing.T) {
	handler, room, user, chat, dir := attachmentHandler(t)
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 64)...)

	var created AttachmentResponse
	rec := upload(t, handler, "/api/v1/rooms/"+room.ID+"/attachments", "../shot.PNG", "", png, &created)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: status = %d; body: %s", rec.Code, rec.Body)
	}
	att := created.Attachment
	if att.Filename != "shot.PNG" || att.MediaType != "image/png" || att.Size != int64(len(png)) || att.RoomID != room.ID {
		t.Errorf("attachment = %+v", att)
	}
	if !strings.HasPrefix(att.ID, "") || att.ID == "" {
		t.Errorf("attachment id missing: %+v", att)
	}
	// The bytes are on disk under the room, named after the id.
	stored, err := os.ReadFile(filepath.Join(dir, room.ID, att.ID+".png"))
	if err != nil || !bytes.Equal(stored, png) {
		t.Errorf("stored file: %v (%d bytes)", err, len(stored))
	}

	rec = get(t, handler, "/api/v1/attachments/"+att.ID)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), png) {
		t.Fatalf("serve: status = %d, %d bytes", rec.Code, rec.Body.Len())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "inline;") || !strings.Contains(cd, "shot.PNG") {
		t.Errorf("Content-Disposition = %q, want inline with the filename", cd)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); csp != "sandbox" {
		t.Errorf("Content-Security-Policy = %q", csp)
	}

	// A message carries the upload by id, with or without words.
	var posted MessageResponse
	body := fmt.Sprintf(`{"user_id":%q,"body":"","attachment_ids":[%q]}`, user.ID, att.ID)
	if rec := do(t, handler, http.MethodPost, "/api/v1/rooms/"+room.ID+"/messages", body, &posted); rec.Code != http.StatusCreated {
		t.Fatalf("post: status = %d; body: %s", rec.Code, rec.Body)
	}
	if len(chat.posted) != 1 || len(chat.posted[0].AttachmentIDs) != 1 || chat.posted[0].AttachmentIDs[0] != att.ID {
		t.Errorf("hub received %+v", chat.posted)
	}
}

func TestAttachments_RefusesWhatItCannotStore(t *testing.T) {
	handler, room, user, _, _ := attachmentHandler(t)

	if rec := upload(t, handler, "/api/v1/rooms/nope/attachments", "a.txt", "text/plain", []byte("x"), nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown room: status = %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+room.ID+"/attachments", strings.NewReader(`{"not":"multipart"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("json body: status = %d", rec.Code)
	}
	if rec := get(t, handler, "/api/v1/attachments/"+store.NewID()); rec.Code != http.StatusNotFound {
		t.Errorf("unknown attachment: status = %d", rec.Code)
	}
	// Neither words nor files is not a message.
	body := fmt.Sprintf(`{"user_id":%q,"body":"  ","attachment_ids":[]}`, user.ID)
	if rec := do(t, handler, http.MethodPost, "/api/v1/rooms/"+room.ID+"/messages", body, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("empty post: status = %d; body: %s", rec.Code, rec.Body)
	}

	// Scripts never run as this origin: HTML downloads instead of rendering.
	var created AttachmentResponse
	if rec := upload(t, handler, "/api/v1/rooms/"+room.ID+"/attachments", "page.html", "text/html", []byte("<script>1</script>"), &created); rec.Code != http.StatusCreated {
		t.Fatalf("upload html: status = %d; body: %s", rec.Code, rec.Body)
	}
	rec = get(t, handler, "/api/v1/attachments/"+created.Attachment.ID)
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") {
		t.Errorf("html Content-Disposition = %q, want attachment", cd)
	}
}

func TestCleanFilename(t *testing.T) {
	cases := map[string]string{
		"report.pdf":           "report.pdf",
		`C:\Users\me\shot.png`: "shot.png",
		"../../etc/passwd":     "passwd",
		"  ":                   "file",
		"..":                   "file",
		"a\x00b.txt":           "ab.txt",
		"图 1.png":              "图 1.png",
		"weird.tar.gz":         "weird.tar.gz",
		"noext":                "noext",
	}
	for in, want := range cases {
		if got := cleanFilename(in); got != want {
			t.Errorf("cleanFilename(%q) = %q, want %q", in, got, want)
		}
	}
	if got := safeExt("archive.TAR.GZ"); got != ".gz" {
		t.Errorf("safeExt = %q", got)
	}
	if got := safeExt("odd.p n g"); got != "" {
		t.Errorf("safeExt with spaces = %q", got)
	}
	if got := safeExt("图.png"); got != ".png" {
		t.Errorf("safeExt = %q", got)
	}
}
