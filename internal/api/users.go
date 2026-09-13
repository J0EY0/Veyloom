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
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name, err := requireName("name", req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
