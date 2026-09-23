package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/J0EY0/veyloom/internal/hub"
)

func (f *fakeWikis) WikiGraph(_ context.Context, projectID string) (hub.WikiGraph, error) {
	if err := f.project(projectID); err != nil {
		return hub.WikiGraph{}, err
	}
	return hub.WikiGraph{
		Nodes: []hub.GraphNode{{ID: "/facts/a.md", Kind: "page"}, {ID: "file:go.mod", Kind: "file", File: "go.mod", Pages: 1}},
		Edges: []hub.GraphEdge{{From: "/facts/a.md", To: "file:go.mod", Kind: "names"}},
	}, nil
}

func (f *fakeWikis) LibraryGraph(context.Context) (hub.WikiGraph, error) {
	return hub.WikiGraph{Nodes: []hub.GraphNode{{ID: "/skills/t/SKILL.md", Kind: "page"}}, Edges: []hub.GraphEdge{}}, nil
}

func TestWikiGraphs(t *testing.T) {
	handler, _ := wikiHandler()
	var got WikiGraphResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/projects/p1/wiki/graph", "", &got); rec.Code != http.StatusOK || len(got.Graph.Nodes) != 2 || got.Graph.Edges[0].Kind != "names" {
		t.Errorf("a project's: %d %+v", rec.Code, got)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/projects/p404/wiki/graph", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("no such project: %d", rec.Code)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/library/graph", "", &got); rec.Code != http.StatusOK || got.Graph.Nodes[0].ID != "/skills/t/SKILL.md" {
		t.Errorf("the library's: %d %+v", rec.Code, got)
	}
	if rec := do(t, NewHandler(Deps{}), http.MethodGet, "/api/v1/library/graph", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("without wikis: %d", rec.Code)
	}
}
