// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fulltext_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/infra/fulltext"
)

// broadProbeGraph answers the broad-class probe with a fixed candidate count and
// records every statement, so the tests can pin the probe-then-read order.
type broadProbeGraph struct {
	searchGraph
	probeCount int64
	probeCalls int
	mainCalls  int
}

func (g *broadProbeGraph) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if strings.Contains(cypher, "candidate_count") {
		g.probeCalls++
		g.calls = append(g.calls, cypher)
		g.params = append(g.params, params)
		return []map[string]any{{"candidate_count": g.probeCount}}, nil
	}
	g.mainCalls++
	return g.searchGraph.Run(ctx, cypher, params)
}

func broadRequest(query string) fulltext.Request {
	req := baseRequest()
	req.Query = query
	return req
}

// A broad single-character query whose probe reports more than K candidates
// must run the capped statement and report the cap hit.
func TestRowsBoundedBroadOverCapReturnsCappedRowsWithMarker(t *testing.T) {
	t.Parallel()
	graph := &broadProbeGraph{probeCount: fulltext.BroadCandidateCap + 1}
	graph.ftRows = []map[string]any{{"name": "a"}}
	rows, capped, err := readyReader(t, nil).RowsBounded(context.Background(), graph, broadRequest("a"))
	if err != nil {
		t.Fatalf("RowsBounded() error = %v, want nil", err)
	}
	if !capped {
		t.Fatal("capped = false, want true when candidates exceed K")
	}
	if graph.probeCalls != 1 || graph.mainCalls != 1 {
		t.Fatalf("probe calls = %d, main calls = %d, want exactly one probe then one read", graph.probeCalls, graph.mainCalls)
	}
	main := graph.calls[1]
	if !strings.Contains(main, "WITH n LIMIT "+strconv.Itoa(fulltext.BroadCandidateCap)) {
		t.Fatalf("capped statement must bound post-index evaluation at K rows:\n%s", main)
	}
	if !strings.Contains(main, "RETURN DISTINCT") {
		t.Fatalf("capped unscoped statement must keep DISTINCT semantics:\n%s", main)
	}
	if len(rows) != 1 || rows[0]["name"] != "a" {
		t.Fatalf("rows = %v, want the capped read rows", rows)
	}
}

// At exactly K candidates the read must be the unchanged statement with no
// marker: rows equal the uncapped read.
func TestRowsBoundedBroadAtCapIsUncappedWithoutMarker(t *testing.T) {
	t.Parallel()
	graph := &broadProbeGraph{probeCount: fulltext.BroadCandidateCap}
	graph.ftRows = []map[string]any{{"name": "a"}}
	req := broadRequest("a")
	rows, capped, err := readyReader(t, nil).RowsBounded(context.Background(), graph, req)
	if err != nil {
		t.Fatalf("RowsBounded() error = %v, want nil", err)
	}
	if capped {
		t.Fatal("capped = true, want false when candidates fit in K")
	}
	want := fulltext.UnscopedStatement(req.Labels, req.WhereExtra, req.ReturnExprs)
	if graph.calls[1] != want {
		t.Fatalf("at-cap statement =\n%s\nwant the byte-identical uncapped statement\n%s", graph.calls[1], want)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %v, want the uncapped rows", rows)
	}
}

// A selective issue term must take the single unchanged full-text read with
// no probe and no marker.
func TestRowsBoundedSelectiveIsSingleUncappedRead(t *testing.T) {
	t.Parallel()
	graph := &broadProbeGraph{probeCount: fulltext.BroadCandidateCap + 1}
	graph.ftRows = []map[string]any{{"name": "ft"}}
	req := baseRequest()
	rows, capped, err := readyReader(t, nil).RowsBounded(context.Background(), graph, req)
	if err != nil {
		t.Fatalf("RowsBounded() error = %v, want nil", err)
	}
	if capped {
		t.Fatal("capped = true, want false for a selective term")
	}
	if graph.probeCalls != 0 || len(graph.calls) != 1 {
		t.Fatalf("calls = %q, want one uncapped full-text read and no probe", graph.calls)
	}
	want := fulltext.UnscopedStatement(req.Labels, req.WhereExtra, req.ReturnExprs)
	if graph.calls[0] != want {
		t.Fatalf("selective statement =\n%s\nwant the byte-identical statement\n%s", graph.calls[0], want)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %v, want the full-text rows", rows)
	}
}

// The "::" equality branch stays selective even when short: it seeks exact
// values instead of running the multi-field CONTAINS recheck.
func TestRowsBoundedEqualityBranchIsNeverBroad(t *testing.T) {
	t.Parallel()
	graph := &broadProbeGraph{probeCount: fulltext.BroadCandidateCap + 1}
	if fulltext.Broad("::") {
		t.Fatal(`Broad("::") = true, want false for the equality branch`)
	}
	req := broadRequest("::")
	if _, capped, err := readyReader(t, nil).RowsBounded(context.Background(), graph, req); err != nil || capped || graph.probeCalls != 0 {
		t.Fatalf("RowsBounded(::) = capped %v err %v probes %d, want uncapped no-probe read", capped, err, graph.probeCalls)
	}
}

// An empty query keeps the legacy statement with no probe and no marker.
func TestRowsBoundedEmptyQueryIsLegacyWithoutMarker(t *testing.T) {
	t.Parallel()
	graph := &broadProbeGraph{}
	req := broadRequest("")
	rows, capped, err := readyReader(t, nil).RowsBounded(context.Background(), graph, req)
	if err != nil {
		t.Fatalf("RowsBounded() error = %v, want nil", err)
	}
	if capped {
		t.Fatal("capped = true, want false for an empty query")
	}
	if graph.probeCalls != 0 || len(graph.calls) != 1 || graph.calls[0] != "LEGACY" {
		t.Fatalf("calls = %q, want one legacy read and no probe", graph.calls)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %v, want no rows", rows)
	}
}

func TestBroadClassifier(t *testing.T) {
	t.Parallel()
	for query, want := range map[string]bool{
		"a":            true,
		"ab":           true,
		"svc":          false,
		"aws_iam_role": false,
		"":             false,
		"::":           false,
		"a::b":         false,
	} {
		if got := fulltext.Broad(query); got != want {
			t.Errorf("Broad(%q) = %v, want %v", query, got, want)
		}
	}
}

func TestCappedStatementsBoundEvaluationBeforeRecheck(t *testing.T) {
	t.Parallel()
	labels := []string{"A", "B"}
	extra := "\n AND x"
	exprs := []string{"coalesce(n.name, '') as name"}
	unscoped := fulltext.CappedUnscopedStatement(labels, extra, exprs)
	if !strings.Contains(unscoped, "WITH n LIMIT "+strconv.Itoa(fulltext.BroadCandidateCap)) {
		t.Fatalf("capped unscoped statement must bound evaluation at K:\n%s", unscoped)
	}
	labelFilter := strings.Index(unscoped, "WHERE (n:A OR n:B)")
	capIdx := strings.Index(unscoped, "WITH n LIMIT ")
	recheckIdx := strings.Index(unscoped, extra)
	if labelFilter >= capIdx || capIdx >= recheckIdx {
		t.Fatalf("cap must sit between the label filter and the recheck:\n%s", unscoped)
	}
	if !strings.Contains(unscoped, "RETURN DISTINCT") || !strings.Contains(unscoped, "ORDER BY name\nLIMIT $limit") {
		t.Fatalf("capped unscoped statement must keep DISTINCT plus ordered paging:\n%s", unscoped)
	}
	scoped := fulltext.CappedScopedStatement(labels, extra, "(GRANT)", exprs)
	if !strings.Contains(scoped, "WITH n LIMIT "+strconv.Itoa(fulltext.BroadCandidateCap)) {
		t.Fatalf("capped scoped statement must bound evaluation at K:\n%s", scoped)
	}
	if strings.Contains(scoped, "DISTINCT") {
		t.Fatalf("capped scoped statement must not de-duplicate per-node rows:\n%s", scoped)
	}
	if !strings.Contains(scoped, "WITH n\nWHERE (GRANT)") {
		t.Fatalf("capped scoped statement must keep the hoisted grant predicate:\n%s", scoped)
	}
	probe := fulltext.ProbeStatement(labels)
	if !strings.Contains(probe, "candidate_count") || !strings.Contains(probe, "LIMIT "+strconv.Itoa(fulltext.BroadCandidateCap+1)) {
		t.Fatalf("probe must count label-matching candidates up to K+1:\n%s", probe)
	}
	if strings.Contains(probe, "CONTAINS") || strings.Contains(probe, "ORDER BY") || strings.Contains(probe, "DISTINCT") {
		t.Fatalf("probe must not evaluate the recheck, sort, or de-duplicate:\n%s", probe)
	}
}
