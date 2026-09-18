package account

import (
	"context"

	"github.com/J0EY0/veyloom/internal/store"
)

// Directory is the store with the account laid over the users table:
// asked about the account's id it answers from the file, about anyone
// else from the table, and it lists the account first. The hub and the
// API resolve names through it.
type Directory struct {
	*store.Store
	File *File
}

// GetUser returns the account for its id, else the users row.
func (d *Directory) GetUser(ctx context.Context, id string) (store.User, error) {
	if u, ok := d.File.Account(); ok && u.ID == id {
		return u, nil
	}
	return d.Store.GetUser(ctx, id)
}

// ListUsers lists the account followed by the users table.
func (d *Directory) ListUsers(ctx context.Context) ([]store.User, error) {
	users, err := d.Store.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	if u, ok := d.File.Account(); ok {
		users = append([]store.User{u}, users...)
	}
	return users, nil
}

// RenameUser renames the account in the file, anyone else in the table.
func (d *Directory) RenameUser(ctx context.Context, id, name string) (store.User, error) {
	if u, ok := d.File.Account(); ok && u.ID == id {
		return d.File.Rename(name)
	}
	return d.Store.RenameUser(ctx, id, name)
}
