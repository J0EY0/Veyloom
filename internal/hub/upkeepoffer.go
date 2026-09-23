package hub

import (
	"context"
	"fmt"

	"github.com/J0EY0/veyloom/internal/store"
)

// Offering a wiki maintainer (docs/design.md 5.16). A project that got no
// maintainer when it was created is offered one in its chat, once, as soon
// as UpkeepOfferTopics of its topics have gone quiet with turns nobody went
// over. The chat draws the note as a card, to choose a member or say no;
// either way it is not offered again.

// maintainerOffer is the offer as the chat's record has it, for whoever
// reads the room as text.
func maintainerOffer(topics int) string {
	return fmt.Sprintf("%d topics of this chat have turns nobody has gone over for the project wiki yet. "+
		"A member can keep the wiki: it goes over what the chat did, once a day unless a person says otherwise, and records what later work needs. "+
		"A person chooses one here, or says no.", topics)
}

// offerMaintainers offers a maintainer to each project due for one.
func (h *Hub) offerMaintainers(ctx context.Context) {
	if h.cfg.UpkeepOfferTopics < 0 {
		return
	}
	projects, err := h.store.ListUnmaintainedProjects(ctx)
	if err != nil {
		h.logger.Error("find the projects to offer a wiki maintainer", "err", err)
		return
	}
	quietSince := h.now().Add(-h.cfg.UpkeepIdle)
	for _, project := range projects {
		if project.MainRoomID == "" {
			continue
		}
		topics, err := h.store.CountSettledTopicsWaiting(ctx, project.ID, quietSince)
		if err != nil {
			h.logger.Error("count the topics waiting for a wiki maintainer", "project", project.ID, "err", err)
			continue
		}
		if topics < h.cfg.UpkeepOfferTopics {
			continue
		}
		if err := h.offerMaintainer(ctx, project, topics); err != nil {
			h.logger.Error("offer a wiki maintainer", "project", project.ID, "err", err)
		}
	}
}

// offerMaintainer posts the offer in the project's chat. The project names
// the note before the chat hears of it: the chat fetches the project again
// on a note from the system, and so knows to draw this one as the card.
func (h *Hub) offerMaintainer(ctx context.Context, project store.Project, topics int) error {
	note, err := h.store.CreateMessage(ctx, store.NewMessage{RoomID: project.MainRoomID, SenderKind: store.SenderSystem, Body: maintainerOffer(topics)})
	if err != nil {
		return err
	}
	offered, err := h.store.SetProjectWikiOffer(ctx, project.ID, note.ID)
	if err != nil {
		return err
	}
	if !offered {
		// A person chose a maintainer, or said no, in the meantime: the note
		// stays in the record unannounced, standing for nothing.
		h.logger.Info("a wiki maintainer was settled while being offered", "project", project.ID)
		return nil
	}
	h.turns.publish(messageEvent(note))
	return nil
}
