package api

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// MachineMembersResponse is the body of GET /api/v1/machines/{id}/members:
// the current members the machine runs, across every project.
type MachineMembersResponse struct {
	Members []store.MachineMember `json:"members"`
}

// MachineActivityResponse is the body of GET /api/v1/machines/{id}/activity:
// the machine's turns over a range, hour by hour or day by day.
type MachineActivityResponse struct {
	Activity store.MachineActivity `json:"activity"`
}

// listMachineMembers answers what a machine runs. It reads the store, not
// the hub, so a machine that is not connected still answers.
func (h *handlers) listMachineMembers(w http.ResponseWriter, r *http.Request) {
	members, err := h.deps.Agents.ListMachineMembers(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, MachineMembersResponse{Members: members})
}

// machineActivity answers what a machine did up to now. ?range= is 24h
// (when left out), 7d or 30d; ?runtime= keeps one runtime's turns; ?tz= is
// the IANA time zone whose hours and days the buckets follow, UTC when left
// out, so a browser gets days that begin at its own midnight.
func (h *handlers) machineActivity(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	q := store.ActivityQuery{Range: store.ActivityRange(params.Get("range")), Runtime: params.Get("runtime"), Location: time.UTC, Now: time.Now()}
	if q.Range == "" {
		q.Range = store.ActivityDay
	}
	if !q.Range.Valid() {
		writeError(w, http.StatusBadRequest, "range must be one of 24h, 7d, 30d")
		return
	}
	if q.Runtime != "" && !slices.Contains(runtime.Names(), q.Runtime) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("runtime must be one of %s", strings.Join(runtime.Names(), ", ")))
		return
	}
	if tz := params.Get("tz"); tz != "" {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown time zone %q", tz))
			return
		}
		q.Location = loc
	}
	activity, err := h.deps.Turns.MachineActivity(r.Context(), r.PathValue("id"), q)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, MachineActivityResponse{Activity: activity})
}
