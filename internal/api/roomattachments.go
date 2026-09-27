package api

import (
	"archive/zip"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
)

// RoomAttachmentsResponse is the body of GET /api/v1/rooms/{id}/attachments:
// a page of the room's attachments, and how many match in all.
type RoomAttachmentsResponse struct {
	Attachments []store.RoomAttachment `json:"attachments"`
	Total       int                    `json:"total"`
}

// archiveMax caps how many attachments one download packs.
const archiveMax = 500

// listRoomAttachments lists the attachments messages of a room carry for
// the attachments tab (docs/webui.md 4.21): q= words, kind= kinds split by
// commas, sender=user:<id> or member:<id>, sort=newest|oldest|size|name,
// offset= and limit=.
func (h *handlers) listRoomAttachments(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("id")
	if _, err := h.deps.Projects.GetRoom(r.Context(), roomID); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	if h.deps.Attachments == nil {
		writeJSON(w, http.StatusOK, RoomAttachmentsResponse{Attachments: []store.RoomAttachment{}})
		return
	}
	v := r.URL.Query()
	q := store.AttachmentQuery{Words: strings.TrimSpace(v.Get("q")), Sort: v.Get("sort")}
	for _, k := range strings.Split(v.Get("kind"), ",") {
		if k = strings.TrimSpace(k); k != "" {
			q.Kinds = append(q.Kinds, k)
		}
	}
	if sender := v.Get("sender"); sender != "" {
		kind, id, _ := strings.Cut(sender, ":")
		switch kind {
		case "user":
			q.UserID = id
		case "member":
			q.MemberID = id
		default:
			writeError(w, http.StatusBadRequest, "sender is user:<id> or member:<id>")
			return
		}
	}
	for key, dst := range map[string]*int{"offset": &q.Offset, "limit": &q.Limit} {
		n, _, err := queryInt(v.Get(key))
		if err != nil {
			writeReason(w, http.StatusBadRequest, fmt.Errorf("%s: %w", key, err))
			return
		}
		*dst = int(n)
	}

	// People's names are in the account file, not the database: the query
	// takes them along, to match the words and name who sent each file.
	if h.deps.Users != nil {
		users, err := h.deps.Users.ListUsers(r.Context())
		if err != nil {
			h.writeStoreError(w, r, err)
			return
		}
		q.People = make(map[string]string, len(users))
		for _, u := range users {
			q.People[u.ID] = u.Name
		}
	}
	found, total, err := h.deps.Attachments.ListRoomAttachments(r.Context(), roomID, q)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, RoomAttachmentsResponse{Attachments: found, Total: total})
}

// downloadRoomAttachments packs attachments of a room into one zip, sent
// as it is written: GET /api/v1/rooms/{id}/attachments/archive?ids=a,b.
func (h *handlers) downloadRoomAttachments(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("id")
	room, err := h.deps.Projects.GetRoom(r.Context(), roomID)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	var ids []string
	for _, id := range strings.Split(r.URL.Query().Get("ids"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 || len(ids) > archiveMax {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("ids names 1 to %d attachments", archiveMax))
		return
	}
	if h.deps.Attachments == nil {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	found, err := h.deps.Attachments.RoomAttachmentsByID(r.Context(), roomID, ids)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	if len(found) == 0 {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}

	name := "attachments"
	if project, err := h.deps.Projects.GetProject(r.Context(), room.ProjectID); err == nil {
		name = project.Name
	}
	name = fmt.Sprintf("%s %s.zip", name, time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	zw := zip.NewWriter(w)
	taken := map[string]int{}
	for _, att := range found {
		if err := h.addToZip(zw, att, uniqueName(taken, att.Filename)); err != nil {
			// The headers are gone: all that is left is to stop.
			h.deps.Logger.Error("pack attachments", "room", roomID, "attachment", att.ID, "err", err)
			return
		}
	}
	if err := zw.Close(); err != nil {
		h.deps.Logger.Error("pack attachments", "room", roomID, "err", err)
	}
}

func (h *handlers) addToZip(zw *zip.Writer, att store.Attachment, name string) error {
	file, err := os.Open(filepath.Join(h.deps.AttachmentDir, filepath.FromSlash(att.Path)))
	if err != nil {
		return err
	}
	defer file.Close()
	method := zip.Deflate
	if packed(att.Kind) {
		method = zip.Store
	}
	out, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: att.CreatedAt})
	if err != nil {
		return err
	}
	_, err = io.Copy(out, file)
	return err
}

// packed says a kind of file is compressed already, not worth deflating
// again.
func packed(kind string) bool {
	switch kind {
	case store.AttachmentImage, store.AttachmentVideo, store.AttachmentAudio, store.AttachmentPDF, store.AttachmentOffice, store.AttachmentArchive:
		return true
	}
	return false
}

// uniqueName gives each file in a zip its own name: a second "a.png" is
// "a (2).png".
func uniqueName(taken map[string]int, filename string) string {
	taken[filename]++
	n := taken[filename]
	if n == 1 {
		return filename
	}
	ext := filepath.Ext(filename)
	name := strings.TrimSuffix(filename, ext) + " (" + strconv.Itoa(n) + ")" + ext
	if taken[name] > 0 {
		return uniqueName(taken, name)
	}
	taken[name]++
	return name
}
