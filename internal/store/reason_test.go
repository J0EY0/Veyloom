package store_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

func TestReason(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		// The reason after the sentinel, whatever came before it.
		{fmt.Errorf("%w: /data/catalog is not a folder on this machine", store.ErrInvalidInput), "/data/catalog is not a folder on this machine"},
		{fmt.Errorf("member m1: %w: a turn is still running", store.ErrConflict), "a turn is still running"},
		{fmt.Errorf("propose: %w", fmt.Errorf("%w: a change waits: let it be decided first", store.ErrConflict)), "a change waits: let it be decided first"},
		{fmt.Errorf("%w: %w", store.ErrInvalidInput, errors.New("this project has no wiki maintainer")), "this project has no wiki maintainer"},
		// None given: the words stay, less the package's name.
		{fmt.Errorf("agent a1: %w", store.ErrNotFound), "agent a1: not found"},
		{store.ErrNotFound, "not found"},
		{fmt.Errorf("%w: ", store.ErrConflict), "conflict: "},
		// Not the store's.
		{errors.New("invalid JSON body: unexpected EOF"), "invalid JSON body: unexpected EOF"},
	} {
		if got := store.Reason(c.err); got != c.want {
			t.Errorf("Reason(%q) = %q, want %q", c.err, got, c.want)
		}
	}
}
