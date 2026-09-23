package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/J0EY0/veyloom/internal/store"
)

// AttachmentStore records uploads and finds them again.
type AttachmentStore interface {
	CreateAttachment(ctx context.Context, a store.NewAttachment) (store.Attachment, error)
	GetAttachment(ctx context.Context, id string) (store.Attachment, error)
}

// AttachmentResponse is the body of POST /api/v1/rooms/{id}/attachments.
type AttachmentResponse struct {
	Attachment store.Attachment `json:"attachment"`
}

const (
	// MaxAttachmentSize bounds one upload.
	MaxAttachmentSize = 20 << 20
	// attachmentField is the multipart field the file arrives in.
	attachmentField = "file"
	// formMemory is how much of a multipart body stays in memory before
	// spilling to temporary files.
	formMemory = 4 << 20
)

// uploadAttachment stores one file for the room and answers with the
// record a message can carry. The bytes are written first, named after the
// id the row will have; a failed insert removes them again.
func (h *handlers) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("id")
	if _, err := h.deps.Projects.GetRoom(r.Context(), roomID); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	if h.deps.Attachments == nil || h.deps.AttachmentDir == "" {
		writeError(w, http.StatusServiceUnavailable, "attachments are not configured")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, MaxAttachmentSize+formMemory)
	if err := r.ParseMultipartForm(formMemory); err != nil {
		var tooBig *http.MaxBytesError
		var disk *fs.PathError
		if errors.As(err, &disk) {
			// The form was fine; keeping it on disk was not.
			h.deps.Logger.Error("read upload", "room", roomID, "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if errors.As(err, &tooBig) {
			writeCoded(w, http.StatusRequestEntityTooLarge, "attachmentTooLarge", store.Params{"mb": strconv.Itoa(MaxAttachmentSize >> 20)}, fmt.Sprintf("file too large (max %d MB)", MaxAttachmentSize>>20))
			return
		}
		writeError(w, http.StatusBadRequest, "expected a multipart form with a file field")
		return
	}
	defer r.MultipartForm.RemoveAll() //nolint:errcheck // temp files only
	file, header, err := r.FormFile(attachmentField)
	if err != nil {
		writeError(w, http.StatusBadRequest, "expected a multipart form with a file field")
		return
	}
	defer file.Close()
	if header.Size > MaxAttachmentSize {
		writeCoded(w, http.StatusRequestEntityTooLarge, "attachmentTooLarge", store.Params{"mb": strconv.Itoa(MaxAttachmentSize >> 20)}, fmt.Sprintf("file too large (max %d MB)", MaxAttachmentSize>>20))
		return
	}

	filename := cleanFilename(header.Filename)
	mediaType, err := detectMediaType(header, file)
	if err != nil {
		h.deps.Logger.Error("read upload", "room", roomID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	id := store.NewID()
	rel := filepath.Join(roomID, id+safeExt(filename))
	abs := filepath.Join(h.deps.AttachmentDir, rel)
	size, err := writeUpload(abs, file)
	if err != nil {
		h.deps.Logger.Error("write upload", "room", roomID, "path", abs, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	att, err := h.deps.Attachments.CreateAttachment(r.Context(), store.NewAttachment{
		ID: id, RoomID: roomID, Filename: filename, MediaType: mediaType, Size: size, Path: filepath.ToSlash(rel),
	})
	if err != nil {
		_ = os.Remove(abs)
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, AttachmentResponse{Attachment: att})
}

// getAttachment serves the bytes. Only media a browser renders harmlessly
// is shown inline; the rest downloads, and nothing runs as this origin.
func (h *handlers) getAttachment(w http.ResponseWriter, r *http.Request) {
	if h.deps.Attachments == nil {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	att, err := h.deps.Attachments.GetAttachment(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	file, err := os.Open(filepath.Join(h.deps.AttachmentDir, filepath.FromSlash(att.Path)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "attachment file missing")
			return
		}
		h.deps.Logger.Error("open attachment", "attachment", att.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		h.deps.Logger.Error("stat attachment", "attachment", att.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	disposition := "attachment"
	if inlineSafe(att.MediaType) {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", att.MediaType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": att.Filename}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	// The bytes behind an id never change.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, att.Filename, info.ModTime(), file)
}

// inlineSafe says whether a media type may be displayed as a page of this
// origin: images, sound, video, PDFs and plain text, but not SVG or HTML,
// which can carry scripts.
func inlineSafe(mediaType string) bool {
	mt, _, err := mime.ParseMediaType(mediaType)
	if err != nil {
		mt = mediaType
	}
	mt = strings.ToLower(mt)
	switch {
	case mt == "image/svg+xml":
		return false
	case strings.HasPrefix(mt, "image/"), strings.HasPrefix(mt, "video/"), strings.HasPrefix(mt, "audio/"):
		return true
	case mt == "application/pdf", mt == "text/plain":
		return true
	}
	return false
}

// cleanFilename keeps just the name the person sees: no directories, no
// control characters, never empty.
func cleanFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	return name
}

// safeExt is the extension worth keeping on disk: short, alphanumeric,
// lower case. Anything else is dropped; the media type is in the row.
func safeExt(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if len(ext) < 2 || len(ext) > 10 {
		return ""
	}
	for _, r := range ext[1:] {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return ""
		}
	}
	return ext
}

// detectMediaType trusts the client's content type unless it said nothing
// useful, in which case the first bytes decide.
func detectMediaType(header *multipart.FileHeader, file multipart.File) (string, error) {
	declared := header.Header.Get("Content-Type")
	if mt, _, err := mime.ParseMediaType(declared); err == nil && mt != "" && mt != "application/octet-stream" {
		return mt, nil
	}
	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	mt, _, _ := mime.ParseMediaType(http.DetectContentType(head[:n]))
	if mt == "" {
		mt = "application/octet-stream"
	}
	return mt, nil
}

// writeUpload copies the file to abs, creating the directory, and returns
// how many bytes landed. The name is fresh, so an existing file is a bug.
func writeUpload(abs string, src io.Reader) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return 0, err
	}
	dst, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return 0, err
	}
	size, err := io.Copy(dst, src)
	if closeErr := dst.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(abs)
		return 0, err
	}
	return size, nil
}
