package hub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
)

// What the hub keeps for a while only is swept away once past it: uploads
// a person attached and then did not send, a day on, with their thumbnails
// (docs/webui.md 4.21), which nothing refers to and would sit on the hub's
// disk for ever; what the chat looked up in the wikis, lookupsKept on
// (docs/design.md 5.23.7).
const (
	sweepEvery     = time.Hour
	unclaimedAfter = 24 * time.Hour
	sweepBatch     = 100
)

// RunSweep sweeps once at start and then every sweepEvery, until ctx ends.
func (h *Hub) RunSweep(ctx context.Context) {
	tick := time.NewTicker(sweepEvery)
	defer tick.Stop()
	for {
		h.sweepAttachments(ctx, time.Now().Add(-unclaimedAfter))
		h.pruneLookups(ctx, time.Now().Add(-lookupsKept))
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// sweepAttachments removes the uploads no message took before the given
// time, rows and files, and says how many went.
func (h *Hub) sweepAttachments(ctx context.Context, before time.Time) int {
	if h.cfg.AttachmentDir == "" {
		return 0
	}
	swept := 0
	for {
		left, err := h.store.UnclaimedAttachments(ctx, before, sweepBatch)
		if err != nil {
			h.logger.Error("find uploads nobody sent", "err", err)
			return swept
		}
		for _, a := range left {
			gone, err := h.store.DeleteUnclaimedAttachment(ctx, a.ID)
			if errors.Is(err, store.ErrNotFound) {
				continue // a message took it meanwhile
			}
			if err != nil {
				h.logger.Error("sweep an upload nobody sent", "attachment", a.ID, "err", err)
				return swept
			}
			for _, rel := range []string{gone.Path, gone.ThumbnailPath} {
				if rel == "" {
					continue
				}
				if err := os.Remove(filepath.Join(h.cfg.AttachmentDir, filepath.FromSlash(rel))); err != nil && !errors.Is(err, os.ErrNotExist) {
					h.logger.Warn("remove an upload nobody sent", "attachment", a.ID, "err", err)
				}
			}
			swept++
		}
		if len(left) < sweepBatch {
			return swept
		}
	}
}
