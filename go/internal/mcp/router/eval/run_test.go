// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package eval

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

// scriptedRanker returns a fixed candidate list per question text and records
// the queries it received.
type scriptedRanker struct {
	byIntent map[string][]string
	queries  []router.Query
	err      error
}

func (s *scriptedRanker) Rank(_ context.Context, q router.Query, _ []toolcontract.ToolDefinition,
	_ map[string]catalog.Entry, _ func(string) bool,
) ([]router.Candidate, error) {
	s.queries = append(s.queries, q)
	if s.err != nil {
		return nil, s.err
	}
	var out []router.Candidate
	for _, name := range s.byIntent[q.Intent] {
		out = append(out, router.Candidate{Name: name})
	}
	if len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func testCorpus() *Corpus {
	return &Corpus{Version: SchemaVersion, Questions: []Question{
		{ID: "a-1", Family: "code_lookup", Question: "q top1", Expected: "t1"},
		{ID: "a-2", Family: "code_lookup", Question: "q alternate", Expected: "t2", Alternates: []string{"t2b"}},
		{ID: "a-3", Family: "code_lookup", Question: "q top5", Expected: "t3"},
		{
			ID: "b-1", Family: "dead_code", Question: "q miss", Expected: "t4",
			Anchors: Anchors{RepoID: "orders-api", Environment: "prod"},
		},
		{ID: "b-2", Family: "dead_code", Question: "q empty", Expected: "t5"},
	}}
}

func testRanker() *scriptedRanker {
	return &scriptedRanker{byIntent: map[string][]string{
		"q top1":      {"t1", "x"},
		"q alternate": {"t2b", "t2"},
		"q top5":      {"a", "b", "c", "d", "t3", "e"},
		"q miss":      {"a", "b", "c", "d", "e", "t4"},
		"q empty":     nil,
	}}
}

func TestRunComputesTopOneAndTopFive(t *testing.T) {
	t.Parallel()
	r := testRanker()
	rep, err := Run(context.Background(), r, testCorpus(), Env{Allowed: func(string) bool { return true }})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.Total != 5 || rep.Top1 != 2 || rep.Top5 != 3 {
		t.Fatalf("overall = %d total, %d top1, %d top5; want 5, 2, 3", rep.Total, rep.Top1, rep.Top5)
	}
	code := rep.Families["code_lookup"]
	if code.Total != 3 || code.Top1 != 2 || code.Top5 != 3 {
		t.Fatalf("code_lookup = %+v, want 3 total, 2 top1, 3 top5", code)
	}
	dead := rep.Families["dead_code"]
	if dead.Total != 2 || dead.Top1 != 0 || dead.Top5 != 0 {
		t.Fatalf("dead_code = %+v, want 2 total, 0 top1, 0 top5", dead)
	}
}

func TestRunListsMissesWithRankAndCandidates(t *testing.T) {
	t.Parallel()
	rep, err := Run(context.Background(), testRanker(), testCorpus(), Env{Allowed: func(string) bool { return true }})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var ids []string
	for _, m := range rep.Misses {
		ids = append(ids, m.ID)
	}
	if got, want := strings.Join(ids, ","), "a-3,b-1,b-2"; got != want {
		t.Fatalf("miss ids = %s, want %s (corpus order)", got, want)
	}
	if rep.Misses[0].Rank != 5 || rep.Misses[1].Rank != 0 {
		t.Fatalf("ranks = %d, %d; want 5 for the top-5 miss and 0 for an absent tool",
			rep.Misses[0].Rank, rep.Misses[1].Rank)
	}
	if len(rep.Misses[1].Got) != 5 {
		t.Fatalf("Got = %v, want the first 5 candidates", rep.Misses[1].Got)
	}
}

func TestRunPassesLimitFiveAndAnchors(t *testing.T) {
	t.Parallel()
	r := testRanker()
	if _, err := Run(context.Background(), r, testCorpus(), Env{Allowed: func(string) bool { return true }}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, q := range r.queries {
		if q.Limit != TopK {
			t.Fatalf("query %q limit = %d, want %d", q.Intent, q.Limit, TopK)
		}
	}
	for _, q := range r.queries {
		if q.Intent == "q miss" && (q.Anchors.RepoID != "orders-api" || q.Anchors.Environment != "prod") {
			t.Fatalf("anchors = %+v, want repo and environment passed through", q.Anchors)
		}
	}
}

func TestRunPropagatesRankerError(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	_, err := Run(context.Background(), &scriptedRanker{err: boom}, testCorpus(), Env{Allowed: func(string) bool { return true }})
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "a-1") {
		t.Fatalf("err = %v, want it to wrap boom and name question a-1", err)
	}
}

func TestRoutableDeniesRouterSurfaces(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"ask", "find_tool", "call_tool"} {
		if Routable(name) {
			t.Errorf("Routable(%q) = true, want false", name)
		}
	}
	if !Routable("find_symbol") {
		t.Error("Routable(find_symbol) = false, want true")
	}
}

func TestTopKTracksFindToolDefaultLimit(t *testing.T) {
	t.Parallel()
	if TopK != router.DefaultLimit {
		t.Fatalf("TopK = %d, want router.DefaultLimit (%d)", TopK, router.DefaultLimit)
	}
}

func TestRoutableUsesRouterToolNames(t *testing.T) {
	t.Parallel()
	for _, name := range []string{router.FindToolName, router.CallToolName} {
		if Routable(name) {
			t.Errorf("Routable(%q) = true, want false", name)
		}
	}
}

func TestRunRejectsNilCorpus(t *testing.T) {
	t.Parallel()
	if _, err := Run(context.Background(), testRanker(), nil, Env{Allowed: Routable}); err == nil {
		t.Fatal("nil corpus: want an error")
	}
}

func TestRunRejectsNilRankerAndNilAllowed(t *testing.T) {
	t.Parallel()
	if _, err := Run(context.Background(), nil, testCorpus(), Env{Allowed: func(string) bool { return true }}); err == nil {
		t.Fatal("nil ranker: want an error")
	}
	if _, err := Run(context.Background(), testRanker(), testCorpus(), Env{}); err == nil {
		t.Fatal("nil Allowed: want an error")
	}
}

func TestFormatPrintsNumbersFamiliesAndMisses(t *testing.T) {
	t.Parallel()
	rep, err := Run(context.Background(), testRanker(), testCorpus(), Env{Allowed: func(string) bool { return true }})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var buf bytes.Buffer
	if err := Format(&buf, rep); err != nil {
		t.Fatalf("Format: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"top-1 2/5 (40.0%)", "top-5 3/5 (60.0%)",
		"code_lookup", "dead_code",
		"MISS b-1", "expected t4", "got a, b, c, d, e",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
}

func TestFormatIsDeterministic(t *testing.T) {
	t.Parallel()
	render := func() string {
		rep, err := Run(context.Background(), testRanker(), testCorpus(), Env{Allowed: func(string) bool { return true }})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		var buf bytes.Buffer
		if err := Format(&buf, rep); err != nil {
			t.Fatalf("Format: %v", err)
		}
		return buf.String()
	}
	first := render()
	for i := 0; i < 20; i++ {
		if got := render(); got != first {
			t.Fatalf("run %d differs from the first:\n%s\n---\n%s", i, got, first)
		}
	}
}

func TestEvalDoesNotImportMCPRoot(t *testing.T) {
	t.Parallel()
	out, err := goListDeps()
	if err != nil {
		t.Fatalf("go list -deps: %v\n%s", err, out)
	}
	for _, dep := range strings.Fields(string(out)) {
		if dep == "github.com/eshu-hq/eshu/go/internal/mcp" {
			t.Fatal("eval depends on the mcp root package")
		}
	}
}
