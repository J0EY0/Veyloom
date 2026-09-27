package api

import (
	"net/http"
	"testing"
)

// Veyloom's own skills, which every agent has, are listed apart from the
// library's (docs/design.md 5.23.6); with no wikis kept, none.
func TestBuiltinSkills(t *testing.T) {
	var got BuiltinSkillsResponse
	if rec := do(t, NewHandler(Deps{Wikis: &fakeWikis{}}), http.MethodGet, "/api/v1/skills/builtin", "", &got); rec.Code != http.StatusOK ||
		len(got.Skills) != 1 || got.Skills[0].Name != "team-practices" || got.Skills[0].Description == "" {
		t.Errorf("builtin: %d %+v", rec.Code, got)
	}
	var none BuiltinSkillsResponse
	if rec := do(t, NewHandler(Deps{}), http.MethodGet, "/api/v1/skills/builtin", "", &none); rec.Code != http.StatusOK || none.Skills == nil || len(none.Skills) != 0 {
		t.Errorf("none: %d %+v", rec.Code, none)
	}
}
