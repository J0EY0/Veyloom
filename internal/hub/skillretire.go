package hub

import (
	"context"
	"errors"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// People take a skill out of use, or out of the library (docs/design.md
// 5.15). Agents do neither: they set a skill right, or record why it
// misleads as a Pattern page.

// RetireSkill takes the skill called name out of use, retired, or puts it
// back. Retired, it goes to no agent's turns and cannot be installed anew,
// though it stays in the library, and installed where it was, to be put
// back as it was.
func (h *Hub) RetireSkill(ctx context.Context, name string, retired bool, userID string) (WikiPageView, error) {
	if !wikiSlug(name) {
		return WikiPageView{}, store.Invalid("skillBadName", store.Params{"name": name}, "%q is no skill name: lowercase letters, digits and hyphens", name)
	}
	r, err := h.openLibrary(ctx)
	if err != nil {
		return WikiPageView{}, err
	}
	if r.bundle.SkillBusy(name) {
		return WikiPageView{}, store.Conflicting("skillBusy", store.Params{"name": name}, "a turn is changing the skill %s right now; try again when the turn ends", name)
	}
	status, subject := okf.Stable, "Put back in use"
	if retired {
		status, subject = okf.Deprecated, "Retired"
	}
	view, err := h.personWrites(ctx, r, wiki.SkillPath(name), userID, subject, func(w *wiki.Writer, path string) error {
		_, err := w.SetStatus(path, status)
		return err
	})
	if err != nil {
		return WikiPageView{}, err
	}
	return h.LibraryPage(ctx, view.Path)
}

// DeleteSkill removes the skill called name from the library, all of its
// folder, as one commit by the person, which the library's history keeps,
// and takes it off every agent it is installed for. A skill on trial is
// left alone, as for an update: a person keeps or rolls back the change
// first.
func (h *Hub) DeleteSkill(ctx context.Context, name, userID string) error {
	if !wikiSlug(name) {
		return store.Invalid("skillBadName", store.Params{"name": name}, "%q is no skill name: lowercase letters, digits and hyphens", name)
	}
	r, err := h.openLibrary(ctx)
	if err != nil {
		return err
	}
	if _, err := r.bundle.Page(wiki.SkillPath(name)); err != nil {
		return err
	}
	if _, err := h.store.OpenSkillTrial(ctx, name); err == nil {
		return store.Conflicting("skillOnTrial", store.Params{"name": name}, "the skill %s is on trial: keep or roll back the change first", name)
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if r.bundle.SkillBusy(name) {
		return store.Conflicting("skillBusy", store.Params{"name": name}, "a turn is changing the skill %s right now; try again when the turn ends", name)
	}
	person, err := h.personActor(ctx, userID)
	if err != nil {
		return err
	}
	w, err := r.bundle.Writer(person)
	if err != nil {
		return err
	}
	if err := w.RemoveSkill(name); err != nil {
		return err
	}
	if _, err := w.Commit(ctx, "Removed the skill "+name); err != nil {
		return err
	}
	h.changed(r)
	agents, err := h.store.ListSkillAgents(ctx, name)
	if err != nil {
		return err
	}
	for _, agent := range agents {
		if err := h.store.SetAgentSkill(ctx, agent.ID, name, false); err != nil {
			return err
		}
	}
	return nil
}
