package api

import (
	"context"
	"net/http"

	"github.com/J0EY0/veyloom/internal/store"
)

// UserStore persists users.
type UserStore interface {
	CreateUser(ctx context.Context, name string) (store.User, error)
	GetUser(ctx context.Context, id string) (store.User, error)
	ListUsers(ctx context.Context) ([]store.User, error)
	RenameUser(ctx context.Context, id, name string) (store.User, error)
}

// RenameRequest is the body of PATCH /api/v1/me.
type RenameRequest struct {
	Name string `json:"name"`
}

// CreateUserRequest is the body of POST /api/v1/users.
type CreateUserRequest struct {
	Name string `json:"name"`
}

// UserResponse is the body of single-user endpoints.
type UserResponse struct {
	User store.User `json:"user"`
}

// UsersResponse is the body of GET /api/v1/users.
type UsersResponse struct {
	Users []store.User `json:"users"`
}

func (h *handlers) createUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := decodeJSON(r, &req); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	name, err := requireName("name", req.Name)
	if err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}

	user, err := h.deps.Users.CreateUser(r.Context(), name)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, UserResponse{User: user})
}

func (h *handlers) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.deps.Users.ListUsers(r.Context())
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, UsersResponse{Users: users})
}

func (h *handlers) getUser(w http.ResponseWriter, r *http.Request) {
	user, err := h.deps.Users.GetUser(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, UserResponse{User: user})
}

// me answers GET /api/v1/me with the signed-in user (docs/webui.md §4.8).
func (h *handlers) me(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in first")
		return
	}
	writeJSON(w, http.StatusOK, UserResponse{User: user})
}

func (h *handlers) renameMe(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in first")
		return
	}
	var req RenameRequest
	if err := decodeJSON(r, &req); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	name, err := requireName("name", req.Name)
	if err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	renamed, err := h.deps.Users.RenameUser(r.Context(), user.ID, name)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, UserResponse{User: renamed})
}
