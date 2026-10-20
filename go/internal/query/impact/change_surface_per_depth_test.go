// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	testgraph "github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// changeSurfaceDepthRangeRe reads the exact-depth range of one per-depth
// change-surface statement, e.g. [*3..3] or [:DEPENDS_ON*3..3]. A statement
// without an exact-depth range is not a per-depth read.
var changeSurfaceDepthRangeRe = regexp.MustCompile(`\*(\d+)\.\.(\d+)`)

// changeSurfaceDepthRangeOfQuery returns the exact depth a per-depth
// statement reads, or ok=false when the statement carries no exact range.
func changeSurfaceDepthRangeOfQuery(cypher string) (depth int, ok bool) {
	match := changeSurfaceDepthRangeRe.FindStringSubmatch(cypher)
	if match == nil || match[1] != match[2] {
		return 0, false
	}
	var parsed int
	if _, err := fmt.Sscanf(match[1], "%d", &parsed); err != nil {
		return 0, false
	}
	return parsed, true
}

// changeSurfaceDepthTestRow builds one traversal row at an explicit depth.
func changeSurfaceDepthTestRow(id, name string, depth int64) map[string]any {
	return map[string]any{
		"id":      id,
		"name":    name,
		"labels":  []any{"Workload"},
		"repo_id": "repository:owner",
		"depth":   depth,
		"rels": []any{map[string]any{
			"type":       "DEFINES",
			"confidence": float64(1),
			"reason":     "test",
		}},
	}
}

// changeSurfaceDepthServingFake answers each per-depth statement with only
// the rows at that statement's depth, honouring its LIMIT the way the graph
// backend does. Rows at any other depth are never served to a depth query,
// so a test that serves a full multi-depth fixture through this fake proves
// the driver re-requests each depth instead of assuming one read.
type changeSurfaceDepthServingFake struct {
	rows  []map[string]any
	calls []changeSurfaceRunCall
}

func (f *changeSurfaceDepthServingFake) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	f.calls = append(f.calls, changeSurfaceRunCall{cypher: cypher, params: params})
	depth, ok := changeSurfaceDepthRangeOfQuery(cypher)
	if !ok {
		return nil, fmt.Errorf("per-depth fake cannot serve a non-depth query: %.80q", cypher)
	}
	served := make([]map[string]any, 0)
	for _, row := range f.rows {
		if querycontract.IntVal(row, "depth") == depth {
			served = append(served, row)
		}
	}
	if limit, ok := params["limit"].(int); ok && len(served) > limit {
		served = served[:limit]
	}
	return served, nil
}

func (f *changeSurfaceDepthServingFake) RunSingle(
	ctx context.Context,
	cypher string,
	params map[string]any,
) (map[string]any, error) {
	rows, err := f.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func changeSurfaceDepthCallDepths(calls []changeSurfaceRunCall) []int {
	depths := make([]int, 0, len(calls))
	for _, call := range calls {
		depth, ok := changeSurfaceDepthRangeOfQuery(call.cypher)
		if !ok {
			depths = append(depths, -1)
			continue
		}
		depths = append(depths, depth)
	}
	return depths
}

// TestRunChangeSurfaceOutgoingStopsWhenFirstDepthFillsProbe pins the #7677
// fast path: when depth 1 alone returns the full limit+1 probe, no deeper
// statement runs. On the filed anchor this is the whole page: one cheap
// depth-1 read replaces the full [*1..4] enumeration.
func TestRunChangeSurfaceOutgoingStopsWhenFirstDepthFillsProbe(t *testing.T) {
	t.Parallel()

	rows := make([]map[string]any, 0, 11)
	for i := range 11 {
		rows = append(rows, changeSurfaceDepthTestRow(
			fmt.Sprintf("workload:%02d", i), fmt.Sprintf("w-%02d", i), 1))
	}
	fake := &changeSurfaceDepthServingFake{rows: rows}
	handler := &Handler{Neo4j: fake}

	got, err := handler.RunChangeSurfaceOutgoing(
		t.Context(), "(start:Repository {id: $target_id})", "", 4, 10,
		map[string]any{"target_id": "repository:changed"},
		querycontract.RepositoryAccessFilter{AllScopes: true},
	)
	if err != nil {
		t.Fatalf("RunChangeSurfaceOutgoing() error = %v", err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("graph calls = %d, want 1 (depth 1 fills the probe)", len(fake.calls))
	}
	if got := changeSurfaceDepthCallDepths(fake.calls); len(got) != 1 || got[0] != 1 {
		t.Fatalf("call depths = %v, want [1]", got)
	}
	if got, want := fake.calls[0].params["limit"], 11; got != want {
		t.Fatalf("depth-1 LIMIT param = %v, want %v (limit+1 probe)", got, want)
	}
	if len(got) != 11 {
		t.Fatalf("rows = %d, want the 11-row probe page", len(got))
	}
}

// TestRunChangeSurfaceOutgoingContinuesPastExhaustedDepths pins the
// interleaving rule: a depth that returns fewer rows than requested is
// exhausted, and the next depth is read with the remaining need. Depth 2 is
// empty here, so depth 3 runs with the need depth 1 left behind.
func TestRunChangeSurfaceOutgoingContinuesPastExhaustedDepths(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		changeSurfaceDepthTestRow("workload:a", "a", 1),
		changeSurfaceDepthTestRow("workload:b", "b", 1),
		changeSurfaceDepthTestRow("workload:c", "c", 1),
		changeSurfaceDepthTestRow("workload:d", "d", 3),
		changeSurfaceDepthTestRow("workload:e", "e", 3),
	}
	fake := &changeSurfaceDepthServingFake{rows: rows}
	handler := &Handler{Neo4j: fake}

	got, err := handler.RunChangeSurfaceOutgoing(
		t.Context(), "(start:Repository {id: $target_id})", "", 4, 10,
		map[string]any{"target_id": "repository:changed"},
		querycontract.RepositoryAccessFilter{AllScopes: true},
	)
	if err != nil {
		t.Fatalf("RunChangeSurfaceOutgoing() error = %v", err)
	}
	if got := changeSurfaceDepthCallDepths(fake.calls); len(got) != 4 ||
		got[0] != 1 || got[1] != 2 || got[2] != 3 || got[3] != 4 {
		t.Fatalf("call depths = %v, want [1 2 3 4]", got)
	}
	// need starts at 11: depth 1 serves 3 (need 8), depth 2 serves 0
	// (need stays 8), depth 3 serves 2 (need 6), depth 4 serves 0.
	wantLimits := []int{11, 8, 8, 6}
	for i, call := range fake.calls {
		if got := call.params["limit"]; got != wantLimits[i] {
			t.Fatalf("call %d LIMIT param = %v, want %v", i, got, wantLimits[i])
		}
	}
	if len(got) != 5 {
		t.Fatalf("rows = %d, want 5 (3 at depth 1, 2 at depth 3)", len(got))
	}
	for i, row := range got {
		wantDepth := []int{1, 1, 1, 3, 3}[i]
		if d := querycontract.IntVal(row, "depth"); d != wantDepth {
			t.Fatalf("row %d depth = %d, want %d (depth-major merge order)", i, d, wantDepth)
		}
	}
}

// TestRunChangeSurfaceOutgoingStopsWhenProbeFillsMidway pins the other stop
// condition: once the collected rows reach limit+1, deeper statements never
// run even when every depth holds rows.
func TestRunChangeSurfaceOutgoingStopsWhenProbeFillsMidway(t *testing.T) {
	t.Parallel()

	rows := make([]map[string]any, 0)
	for depth := int64(1); depth <= 4; depth++ {
		for i := range 4 {
			rows = append(rows, changeSurfaceDepthTestRow(
				fmt.Sprintf("workload:d%d-%d", depth, i),
				fmt.Sprintf("d%d-%d", depth, i), depth))
		}
	}
	fake := &changeSurfaceDepthServingFake{rows: rows}
	handler := &Handler{Neo4j: fake}

	got, err := handler.RunChangeSurfaceOutgoing(
		t.Context(), "(start:Repository {id: $target_id})", "", 4, 10,
		map[string]any{"target_id": "repository:changed"},
		querycontract.RepositoryAccessFilter{AllScopes: true},
	)
	if err != nil {
		t.Fatalf("RunChangeSurfaceOutgoing() error = %v", err)
	}
	// need 11: d1 serves 4 (need 7), d2 serves 4 (need 3), d3 serves 3
	// (probe full) -> d4 never runs.
	if got := changeSurfaceDepthCallDepths(fake.calls); len(got) != 3 ||
		got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("call depths = %v, want [1 2 3]", got)
	}
	if len(got) != 11 {
		t.Fatalf("rows = %d, want the 11-row probe page", len(got))
	}
}

// TestRunChangeSurfaceConsumersReadsExactDepths pins the per-depth shape on
// the incoming repository-consumers branch: exact-depth typed ranges in
// traversal order with the remaining need as LIMIT.
func TestRunChangeSurfaceConsumersReadsExactDepths(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		changeSurfaceDepthTestRow("repository:a1", "a1", 1),
		changeSurfaceDepthTestRow("repository:a2", "a2", 1),
		changeSurfaceDepthTestRow("repository:a3", "a3", 1),
		changeSurfaceDepthTestRow("repository:c1", "c1", 3),
		changeSurfaceDepthTestRow("repository:c2", "c2", 3),
		changeSurfaceDepthTestRow("repository:c3", "c3", 3),
		changeSurfaceDepthTestRow("repository:c4", "c4", 3),
		changeSurfaceDepthTestRow("repository:c5", "c5", 3),
		changeSurfaceDepthTestRow("repository:d1", "d1", 4),
		changeSurfaceDepthTestRow("repository:d2", "d2", 4),
	}
	fake := &changeSurfaceDepthServingFake{rows: rows}
	handler := &Handler{Neo4j: fake}

	got, err := handler.RunChangeSurfaceRepositoryConsumers(
		t.Context(), "", 4, 10,
		map[string]any{"target_id": "repository:changed"},
		querycontract.RepositoryAccessFilter{AllScopes: true},
	)
	if err != nil {
		t.Fatalf("RunChangeSurfaceRepositoryConsumers() error = %v", err)
	}
	if got := changeSurfaceDepthCallDepths(fake.calls); len(got) != 4 {
		t.Fatalf("call depths = %v, want all 4 depths (10 rows never fill the probe)", got)
	}
	// The consumers branch carries no params["limit"]: the remaining need
	// travels as the inline LIMIT. need starts at 11: depth 1 serves 3
	// (need 8), depth 2 serves 0 (need stays 8), depth 3 serves 5 (need 3),
	// depth 4 serves 2. A need->limit+1 slip must fail here, not silently
	// over-read past the probe.
	wantInlineLimits := []int{11, 8, 8, 3}
	for i, call := range fake.calls {
		if !strings.Contains(call.cypher, "<-[:DEPENDS_ON*") {
			t.Fatalf("consumer statement lost its incoming typed range: %.120q", call.cypher)
		}
		if strings.Contains(call.cypher, "ORDER BY depth") {
			t.Fatalf("per-depth statement must order within its fixed depth, not by depth: %.120q", call.cypher)
		}
		gotLimit, ok := changeSurfaceInlineLimitOfQuery(call.cypher)
		if !ok {
			t.Fatalf("call %d carries no inline LIMIT: %.120q", i, call.cypher)
		}
		if gotLimit != wantInlineLimits[i] {
			t.Fatalf("call %d inline LIMIT = %d, want %d (remaining need)", i, gotLimit, wantInlineLimits[i])
		}
	}
	if len(got) != 10 {
		t.Fatalf("rows = %d, want 10", len(got))
	}
}

// changeSurfaceInlineLimitRe matches the trailing inline LIMIT of a
// per-depth statement, e.g. the `need` the consumers branch Sprintfs.
var changeSurfaceInlineLimitRe = regexp.MustCompile(`LIMIT\s+(\d+)\s*$`)

// changeSurfaceInlineLimitOfQuery returns the inline LIMIT of one per-depth
// statement, or ok=false when the statement carries none.
func changeSurfaceInlineLimitOfQuery(cypher string) (limit int, ok bool) {
	match := changeSurfaceInlineLimitRe.FindStringSubmatch(cypher)
	if match == nil {
		return 0, false
	}
	var parsed int
	if _, err := fmt.Sscanf(match[1], "%d", &parsed); err != nil {
		return 0, false
	}
	return parsed, true
}

// TestRunChangeSurfaceOutgoingPropagatesDepthError pins fail-closed reads: a
// backend error on any depth aborts the whole read instead of returning the
// shallower prefix as if it were complete.
func TestRunChangeSurfaceOutgoingPropagatesDepthError(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	var calls int
	handler := &Handler{Neo4j: testgraph.FakeGraphReader{RunFn: func(
		_ context.Context, cypher string, _ map[string]any,
	) ([]map[string]any, error) {
		calls++
		if depth, ok := changeSurfaceDepthRangeOfQuery(cypher); ok && depth == 2 {
			return nil, boom
		}
		return []map[string]any{changeSurfaceDepthTestRow("workload:a", "a", 1)}, nil
	}}}

	_, err := handler.RunChangeSurfaceOutgoing(
		t.Context(), "(start:Repository {id: $target_id})", "", 4, 10,
		map[string]any{"target_id": "repository:changed"},
		querycontract.RepositoryAccessFilter{AllScopes: true},
	)
	if !errors.Is(err, boom) {
		t.Fatalf("RunChangeSurfaceOutgoing() error = %v, want the depth-2 failure", err)
	}
	if calls != 2 {
		t.Fatalf("graph calls = %d, want 2 (depth 1 served, depth 2 failed)", calls)
	}
}

// TestChangeSurfaceDepthStatementsAreNornicDBSafe extends
// TestChangeSurfaceTraversalQueriesAreNornicDBSafe to every exact depth the
// driver can emit (1..8): each per-depth statement must keep the
// single-anchor-clause shape the pinned NornicDB build executes, with the
// [*d..d] bracket preserving the #6786 pairing exemption (one
// variable-length relationship, never a bracketless fixed-length hop).
func TestChangeSurfaceDepthStatementsAreNornicDBSafe(t *testing.T) {
	t.Parallel()

	envClause := changeSurfaceEnvironmentClause("prod")
	for depth := 1; depth <= 8; depth++ {
		queries := map[string]string{
			"unscoped": fmt.Sprintf(
				changeSurfaceInvestigateCypher,
				"(start:Workload {id: $target_id})", depth, depth, ""),
			"unscoped+env": fmt.Sprintf(
				changeSurfaceInvestigateCypher,
				"(start:Workload {id: $target_id})", depth, depth, envClause),
			"consumers": fmt.Sprintf(
				changeSurfaceRepositoryConsumersCypher, depth, depth, "", 11),
		}
		for name, q := range queries {
			if got := strings.Count(q, "MATCH"); got != 1 {
				t.Errorf("depth %d %s must use exactly one MATCH, got %d: %s", depth, name, got, q)
			}
			for _, banned := range []string{"RETURN DISTINCT", "OPTIONAL", "UNWIND", "WITH ", "| rel."} {
				if strings.Contains(q, banned) {
					t.Errorf("depth %d %s must not contain %q: %s", depth, name, banned, q)
				}
			}
			if !strings.Contains(q, fmt.Sprintf("*%d..%d]", depth, depth)) {
				t.Errorf("depth %d %s must read exactly depth %d: %s", depth, name, depth, q)
			}
			testgraph.AssertCypherHasNoIgnoredLabelPredicate(t, q)
			testgraph.AssertCypherHasNoBrokenAndOr(t, q)
		}
		// The scoped shape keeps its CALL-subquery UNION wrapper per depth;
		// only the label-predicate positions are asserted here (the wrapper
		// is covered by the whitelist-structure guards).
		scoped := fmt.Sprintf(
			changeSurfaceScopedOutgoingCypher,
			"(start:Workload {id: $target_id})", depth, depth, "",
			"(start:Workload {id: $target_id})", depth, depth, "")
		testgraph.AssertCypherHasNoIgnoredLabelPredicate(t, scoped)
		testgraph.AssertCypherHasNoBrokenAndOr(t, scoped)
	}
}

// TestRunChangeSurfaceOutgoingWithNonPositiveDepthRunsNoReads pins the
// degenerate bound: a non-positive max depth reads nothing rather than
// emitting a bogus range.
func TestRunChangeSurfaceOutgoingWithNonPositiveDepthRunsNoReads(t *testing.T) {
	t.Parallel()

	var calls int
	handler := &Handler{Neo4j: testgraph.FakeGraphReader{RunFn: func(
		_ context.Context, _ string, _ map[string]any,
	) ([]map[string]any, error) {
		calls++
		return nil, nil
	}}}

	got, err := handler.RunChangeSurfaceOutgoing(
		t.Context(), "(start:Repository {id: $target_id})", "", 0, 10,
		map[string]any{"target_id": "repository:changed"},
		querycontract.RepositoryAccessFilter{AllScopes: true},
	)
	if err != nil {
		t.Fatalf("RunChangeSurfaceOutgoing() error = %v", err)
	}
	if calls != 0 || len(got) != 0 {
		t.Fatalf("calls = %d rows = %d, want no reads and no rows", calls, len(got))
	}
}
