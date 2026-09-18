package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
)

// Avatars are the pictures people pick for their agents. The browser
// squares and shrinks the image before it is sent; the hub keeps the bytes
// as a file named after them, so a name always means the same picture and
// an agent shows a new one by naming a new file.

// MaxAvatarSize bounds one avatar upload.
const MaxAvatarSize = 1 << 20

// avatarTypes are the images an avatar may be, by the extension its file
// gets.
var avatarTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
}

// avatarName is what an upload is called: the start of its sha256 and the
// extension of its type. Nothing else is looked up on disk.
var avatarName = regexp.MustCompile(`^[0-9a-f]{32}\.(png|jpg|webp)$`)

// AvatarResponse is the body of POST /api/v1/avatars: the name to put in
// an agent's avatar.
type AvatarResponse struct {
	Avatar string `json:"avatar"`
}

// uploadAvatar stores the image in the request body. The first bytes
// decide what it is, not what the client says; the same image uploaded
// twice is one file.
func (h *handlers) uploadAvatar(w http.ResponseWriter, r *http.Request) {
	if h.deps.AvatarDir == "" {
		writeError(w, http.StatusServiceUnavailable, "avatars are not configured")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxAvatarSize))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("image too large (max %d MB)", MaxAvatarSize>>20))
			return
		}
		writeError(w, http.StatusBadRequest, "could not read the image")
		return
	}
	mediaType, _, _ := mime.ParseMediaType(http.DetectContentType(data))
	ext, ok := avatarTypes[mediaType]
	if !ok {
		writeError(w, http.StatusUnsupportedMediaType, "an avatar must be a PNG, JPEG or WebP image")
		return
	}
	sum := sha256.Sum256(data)
	name := hex.EncodeToString(sum[:16]) + ext
	if err := writeAvatar(filepath.Join(h.deps.AvatarDir, name), data); err != nil {
		h.deps.Logger.Error("write avatar", "avatar", name, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, AvatarResponse{Avatar: name})
}

// writeAvatar puts data at path unless it is there already, through a
// temporary file so a half-written picture is never served.
func writeAvatar(path string, data []byte) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".avatar-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // gone after the rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// getAvatar serves an avatar. The bytes behind a name never change.
func (h *handlers) getAvatar(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !h.avatarExists(name) {
		writeError(w, http.StatusNotFound, "avatar not found")
		return
	}
	file, err := os.Open(filepath.Join(h.deps.AvatarDir, name))
	if err != nil {
		h.deps.Logger.Error("open avatar", "avatar", name, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		h.deps.Logger.Error("stat avatar", "avatar", name, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	for mediaType, ext := range avatarTypes {
		if filepath.Ext(name) == ext {
			w.Header().Set("Content-Type", mediaType)
		}
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, name, info.ModTime(), file)
}

// avatarExists reports whether name is an avatar that was uploaded.
func (h *handlers) avatarExists(name string) bool {
	if h.deps.AvatarDir == "" || !avatarName.MatchString(name) {
		return false
	}
	info, err := os.Stat(filepath.Join(h.deps.AvatarDir, name))
	return err == nil && info.Mode().IsRegular()
}

// dropAvatar removes an avatar file once no agent shows it. Failing only
// leaves a small file behind, so it is logged rather than returned.
func (h *handlers) dropAvatar(ctx context.Context, name string) {
	if name == "" || h.deps.AvatarDir == "" || !avatarName.MatchString(name) {
		return
	}
	n, err := h.deps.Agents.AgentsWithAvatar(ctx, name)
	if err != nil {
		h.deps.Logger.Error("count agents with avatar", "avatar", name, "err", err)
		return
	}
	if n > 0 {
		return
	}
	if err := os.Remove(filepath.Join(h.deps.AvatarDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		h.deps.Logger.Error("remove avatar", "avatar", name, "err", err)
	}
}
