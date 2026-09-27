package hub

import (
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// A search no page matches as written offers the pages sharing words with
// it: a question asked whole in Chinese, with no spaces to split it at,
// finds the page it is about.
func TestSearchWiki_OffersThePagesSharingWords(t *testing.T) {
	f := newBriefWiki(t)
	f.page("codex/default", "/decisions/approval-timeout.md", "Decision", "审批超时的处理", "超时按成员的档位处理。", "等人答复超过十分钟算超时。")
	f.page("codex/default", "/pitfalls/migrations.md", "Pitfall", "改迁移后要删库重建", "goose 不会重跑改过的迁移。", "删库重建。")
	tw := &turnWiki{scope: store.WikiProject, bundle: f.bundle}

	got, hits, err := tw.search(wikiArgs{Query: "审批超时怎么处理"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if hits != 0 {
		t.Errorf("a question asked whole matches no page as written: %d", hits)
	}
	wantInOrder(t, got,
		`No page of this project's wiki holds "审批超时怎么处理" as written. Pages sharing words with it, best first:`,
		"/decisions/approval-timeout.md: 审批超时的处理 (Decision",
		"search again with fewer or other words, split by spaces",
	)
	if strings.Contains(got, "/pitfalls/migrations.md") {
		t.Errorf("a page sharing no words is offered:\n%s", got)
	}

	// Words split by spaces are found as before.
	if got, hits, _ = tw.search(wikiArgs{Query: "审批 超时"}, nil); hits == 0 || !strings.HasPrefix(got, `Pages matching "审批 超时", best first:`) {
		t.Errorf("a search by words:\n%s", got)
	}
	// Nothing near says how to search again.
	if got, hits, _ = tw.search(wikiArgs{Query: "部署流水线"}, nil); hits != 0 || !strings.HasPrefix(got, `No page of this project's wiki matches "部署流水线". To find one, search again`) {
		t.Errorf("nothing near:\n%s", got)
	}
}
