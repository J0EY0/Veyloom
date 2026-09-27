package api

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// An uploaded picture is known by its size, and a big one gets a smaller
// copy for the chat; other files get the kind the chat draws them as.
func TestAttachments_PictureDetails(t *testing.T) {
	handler, room, _, _, dir := attachmentHandler(t)
	base := "/api/v1/rooms/" + room.ID + "/attachments"

	var big AttachmentResponse
	if rec := upload(t, handler, base, "screen.png", "image/png", pngOf(t, 1600, 1000), &big); rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	if a := big.Attachment; a.Kind != store.AttachmentImage || a.Width != 1600 || a.Height != 1000 || !a.Thumbnail {
		t.Errorf("big picture: %+v", a)
	}
	if _, err := os.Stat(filepath.Join(dir, room.ID, big.Attachment.ID+".thumb.jpg")); err != nil {
		t.Errorf("thumbnail on disk: %v", err)
	}
	rec := get(t, handler, "/api/v1/attachments/"+big.Attachment.ID+"/thumbnail")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("thumbnail: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if cfg, _, err := image.DecodeConfig(rec.Body); err != nil || cfg.Width != 640 || cfg.Height != 400 {
		t.Errorf("thumbnail picture: %+v %v", cfg, err)
	}

	// A small picture is its own thumbnail.
	var small AttachmentResponse
	upload(t, handler, base, "icon.png", "image/png", pngOf(t, 64, 32), &small)
	if a := small.Attachment; a.Width != 64 || a.Height != 32 || a.Thumbnail {
		t.Errorf("small picture: %+v", a)
	}
	rec = get(t, handler, "/api/v1/attachments/"+small.Attachment.ID+"/thumbnail")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Errorf("small picture's thumbnail: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}

	// Code is text; it has no picture to show.
	var code AttachmentResponse
	upload(t, handler, base, "tags_test.go", "", []byte("package main\n"), &code)
	if a := code.Attachment; a.Kind != store.AttachmentText || a.Width != 0 || a.Thumbnail {
		t.Errorf("code: %+v", a)
	}
	if rec := get(t, handler, "/api/v1/attachments/"+code.Attachment.ID+"/thumbnail"); rec.Code != http.StatusNotFound {
		t.Errorf("code's thumbnail: %d", rec.Code)
	}
}

func TestAttachments_ListRoom(t *testing.T) {
	handler, room, user, _, _, attachments := attachmentsHandler(t)
	attachments.listed = []store.RoomAttachment{
		{Attachment: store.Attachment{ID: "a1", Filename: "screen.png", Kind: store.AttachmentImage}, SenderKind: store.SenderUser, UserID: user.ID, MessageSeq: 7},
		{Attachment: store.Attachment{ID: "a2", Filename: "tags.pdf", Kind: store.AttachmentPDF}, SenderKind: store.SenderAgent, MemberID: "m1", SenderName: "Coder"},
	}
	base := "/api/v1/rooms/" + room.ID + "/attachments"

	var page RoomAttachmentsResponse
	if rec := do(t, handler, http.MethodGet, base+"?q=ALI&kind=image,pdf&sender=member:m1&sort=size&offset=20&limit=10", "", &page); rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	asked := attachments.asked
	if asked.Words != "ALI" || !slices.Equal(asked.Kinds, []string{"image", "pdf"}) || asked.MemberID != "m1" || asked.Sort != "size" ||
		asked.Offset != 20 || asked.Limit != 10 {
		t.Errorf("asked %+v", asked)
	}
	// People's names are only in the account file: the query takes them
	// along, to match the words and to name who sent what.
	if asked.People[user.ID] != "alice" {
		t.Errorf("people: %v", asked.People)
	}
	if page.Total != 42 || len(page.Attachments) != 2 || page.Attachments[1].SenderName != "Coder" {
		t.Errorf("page: %+v", page)
	}

	if rec := get(t, handler, base+"?sender=robot:1"); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown sender kind: %d", rec.Code)
	}
	if rec := get(t, handler, base+"?offset=-1"); rec.Code != http.StatusBadRequest {
		t.Errorf("negative offset: %d", rec.Code)
	}
	if rec := get(t, handler, "/api/v1/rooms/00000000-0000-0000-0000-000000000009/attachments"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown room: %d", rec.Code)
	}
}

// Picked attachments download as one zip, each file under its own name.
func TestAttachments_Archive(t *testing.T) {
	handler, room, _, _, dir, attachments := attachmentsHandler(t)
	put := func(id, name, kind, content string) {
		rel := filepath.Join(room.ID, id+filepath.Ext(name))
		if err := os.MkdirAll(filepath.Join(dir, room.ID), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		attachments.rows[id] = store.Attachment{ID: id, RoomID: room.ID, MessageID: "m", Filename: name, Kind: kind, Path: rel}
	}
	put("a1", "shot.png", store.AttachmentImage, "first picture")
	put("a2", "shot.png", store.AttachmentImage, "second picture")
	put("a3", "notes.md", store.AttachmentText, "# notes\n")
	attachments.rows["other"] = store.Attachment{ID: "other", RoomID: "another room", Filename: "x.txt"}

	rec := get(t, handler, "/api/v1/rooms/"+room.ID+"/attachments/archive?ids=a1,a2,a3,other")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("archive: %d %q %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, `filename="p 20`) {
		t.Errorf("Content-Disposition = %q, want the project's name", cd)
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		got[f.Name] = string(b)
	}
	want := map[string]string{"shot.png": "first picture", "shot (2).png": "second picture", "notes.md": "# notes\n"}
	if len(got) != len(want) {
		t.Errorf("files: %v", got)
	}
	for name, content := range want {
		if got[name] != content {
			t.Errorf("%s = %q, want %q", name, got[name], content)
		}
	}

	if rec := get(t, handler, "/api/v1/rooms/"+room.ID+"/attachments/archive"); rec.Code != http.StatusBadRequest {
		t.Errorf("no ids: %d", rec.Code)
	}
	if rec := get(t, handler, "/api/v1/rooms/"+room.ID+"/attachments/archive?ids=other"); rec.Code != http.StatusNotFound {
		t.Errorf("another room's attachment: %d", rec.Code)
	}
}

func TestUniqueName(t *testing.T) {
	taken := map[string]int{}
	var got []string
	for _, name := range []string{"a.png", "a.png", "a (2).png", "a.png", "README"} {
		got = append(got, uniqueName(taken, name))
	}
	want := []string{"a.png", "a (2).png", "a (2) (2).png", "a (3).png", "README"}
	if !slices.Equal(got, want) {
		t.Errorf("names = %v, want %v", got, want)
	}
}
